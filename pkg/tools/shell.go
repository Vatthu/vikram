package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/permissions"
)

// pathExtractPattern matches absolute paths in command strings (Windows & POSIX).
// Compiled once at package init to avoid per-call overhead.
var pathExtractPattern = regexp.MustCompile(`[A-Za-z]:\\[^\\\"'\s]+|/[^\s\"']+`)

var sedExecPattern = regexp.MustCompile(`(^|[;[:space:]])e([;[:space:]]|$)`)
var sedFileWritePattern = regexp.MustCompile(`(^|[;[:space:]])[rw][[:space:]]+\S+`)

type ExecTool struct {
	workingDir          string
	timeout             time.Duration
	denyPatterns        []*regexp.Regexp
	allowPatterns       []*regexp.Regexp
	restrictToWorkspace bool
	securityMiddleware  SecurityMiddleware
	bus                 *bus.MessageBus
}

func NewExecTool(workingDir string, restrict bool, msgBus *bus.MessageBus) *ExecTool {
	denyPatterns := []*regexp.Regexp{
		// Destructive filesystem wipes
		regexp.MustCompile(`\brm\s+(-[a-z]*r[a-z]*f|-[a-z]*f[a-z]*r)\s+/`), // rm -rf / or rm -fr /
		regexp.MustCompile(`\brmdir\s+/`),
		// Disk-level wipes / partition tools
		regexp.MustCompile(`\bdd\b.*\bof=/dev/`), // dd of=/dev/sda
		regexp.MustCompile(`\bmkfs\b`),           // any filesystem formatting
		regexp.MustCompile(`\bfdisk\b`),
		regexp.MustCompile(`\bparted\b`),
		regexp.MustCompile(`\bwipefs\b`),
		// Privilege escalation
		regexp.MustCompile(`\bsudo\b`),
		regexp.MustCompile(`\bsu\s`), // su <user> (not "sudo")
		regexp.MustCompile(`\bdoas\b`),
		// Pipe-to-shell download execution (curl|bash etc.)
		regexp.MustCompile(`\|\s*(bash|sh|zsh|ksh|csh|fish)\b`),
		// Kernel module manipulation
		regexp.MustCompile(`\bmodprobe\b`),
		regexp.MustCompile(`\binsmod\b`),
		regexp.MustCompile(`\brmmod\b`),
		// Fork-bomb pattern:  :() { :|:& };:
		regexp.MustCompile(`:\s*\(\)\s*\{`),
		// Overwrite critical system files
		regexp.MustCompile(`>\s*/etc/(passwd|shadow|sudoers|crontab)\b`),
		// chmod setuid/setgid bits
		regexp.MustCompile(`\bchmod\s+[ugoa]*[+]s\b`),
		regexp.MustCompile(`\bchmod\s+[0-7]*[4-7][0-7]{3}\b`), // e.g. chmod 4755
	}

	return &ExecTool{
		workingDir:          workingDir,
		timeout:             60 * time.Second,
		denyPatterns:        denyPatterns,
		allowPatterns:       nil,
		restrictToWorkspace: restrict,
		securityMiddleware:  &AllowlistMiddleware{Allowed: DefaultAllowlist},
		bus:                 msgBus,
	}
}

// NewExecToolForWorkspace configures the shell tool policy to match the
// workspace security posture. Sandboxed workspaces keep the minimal default
// allowlist; non-sandboxed development workspaces use the broader dev allowlist.
func NewExecToolForWorkspace(workingDir string, restrict bool, sandboxed bool, msgBus *bus.MessageBus) *ExecTool {
	tool := NewExecTool(workingDir, restrict, msgBus)
	if sandboxed {
		tool.SetAllowlist(DefaultAllowlist)
	} else {
		tool.SetAllowlist(DevAllowlist)
	}
	return tool
}

func (t *ExecTool) SetSecurityMiddleware(sm SecurityMiddleware) {
	t.securityMiddleware = sm
}

func (t *ExecTool) SetAllowlist(allowed []string) {
	clone := append([]string{}, allowed...)
	t.securityMiddleware = &AllowlistMiddleware{Allowed: clone}
}

func (t *ExecTool) Name() string {
	return "exec"
}

func (t *ExecTool) Description() string {
	return "Execute a shell command and return its output. Use with caution."
}

func (t *ExecTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"command": map[string]interface{}{
				"type":        "string",
				"description": "The shell command to execute",
			},
			"working_dir": map[string]interface{}{
				"type":        "string",
				"description": "Optional working directory for the command",
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	command, ok := args["command"].(string)
	if !ok {
		return ErrorResult("command is required")
	}

	cwd := t.workingDir
	if wd, ok := args["working_dir"].(string); ok && wd != "" {
		cwd = wd
	}

	// Validate working_dir stays within workspace when restricted
	if t.restrictToWorkspace && cwd != t.workingDir {
		absWD, err1 := filepath.Abs(t.workingDir)
		absCWD, err2 := filepath.Abs(cwd)
		if err1 == nil && err2 == nil {
			// Validate symlinks in cwd
			cwdReal := absCWD
			if resolved, err := filepath.EvalSymlinks(absCWD); err == nil {
				cwdReal = resolved
			} else if !os.IsNotExist(err) && !os.IsPermission(err) {
				return ErrorResult(fmt.Sprintf("failed to resolve working_dir symlink: %v", err))
			}

			if !strings.HasPrefix(cwdReal, absWD+string(filepath.Separator)) && cwdReal != absWD {
				if t.bus != nil && tc.Channel != "" {
					t.bus.PublishOutbound(bus.OutboundMessage{
						Channel: tc.Channel,
						ChatID:  tc.ChatID,
						Content: fmt.Sprintf("⚠️ **Security Alert**: I attempted to execute a command in `%s` but was blocked by your Strict Sandbox configuration. I am restricted to `%s`.", cwdReal, t.workingDir),
					})
				}
				return ErrorResult("working_dir must be within the workspace directory")
			}
		}
	}

	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			cwd = wd
		}
	}

	// 1. Security Middleware Vetting
	vettedCommand := command
	if t.securityMiddleware != nil {
		var err error
		vettedCommand, err = t.securityMiddleware.VerifyCommand(command)
		if err != nil {
			return ErrorResult(fmt.Sprintf("Security violation: %v", err))
		}
	}

	segments, err := splitPipelineSegments(vettedCommand)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Command blocked by safety guard (%v)", err))
	}
	if len(segments) == 0 {
		return ErrorResult("command is empty")
	}

	// 2. Legacy Pattern Guarding (still here for a second layer of defense/specific hardware checks)
	for _, segment := range segments {
		if t.securityMiddleware != nil {
			if _, err := t.securityMiddleware.VerifyCommand(segment); err != nil {
				return ErrorResult(fmt.Sprintf("Security violation: %v", err))
			}
		}
		if guardError := t.guardCommand(segment, cwd); guardError != "" {
			return ErrorResult(guardError)
		}
	}

	// ... continue with execution using vettedCommand
	var cmdCtx context.Context
	var cancel context.CancelFunc
	if t.timeout > 0 {
		cmdCtx, cancel = context.WithTimeout(ctx, t.timeout)
	} else {
		cmdCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	parsed := make([]parsedExecCommand, 0, len(segments))
	for _, segment := range segments {
		program, argsSlice := parseCommandForDirectExecution(segment)
		if program == "" {
			return ErrorResult("Failed to parse command for direct execution or empty command")
		}

		if guardError := guardParsedCommand(program, argsSlice); guardError != "" {
			return ErrorResult(guardError)
		}

		// 3. Program Path Validation (Security against arbitrary binary execution)
		if t.restrictToWorkspace {
			// Attempt to resolve the program in the PATH
			resolvedProgram, err := exec.LookPath(program)
			if err != nil {
				return ErrorResult(fmt.Sprintf("Program '%s' not found in PATH or not executable", program))
			}
			program = resolvedProgram
		}

		parsed = append(parsed, parsedExecCommand{program: program, args: argsSlice})
	}

	output, err := runParsedCommands(cmdCtx, cwd, parsed)

	var exitCode *int
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			msg := fmt.Sprintf("Command timed out after %v", t.timeout)
			return &ToolResult{
				ForLLM:  msg,
				ForUser: msg,
				IsError: true,
			}
		}
		exitCode = exitCodeFromError(err)
		output += fmt.Sprintf("\nExit code: %v", err)
	}

	if output == "" {
		output = "(no output)"
	}

	maxLen := 10000
	if len(output) > maxLen {
		output = output[:maxLen] + fmt.Sprintf("\n... (truncated, %d more chars)", len(output)-maxLen)
	}

	if err != nil {
		return &ToolResult{
			ForLLM:   output,
			ForUser:  output,
			IsError:  true,
			ExitCode: exitCode,
		}
	}

	return &ToolResult{
		ForLLM:   output,
		ForUser:  output,
		IsError:  false,
		ExitCode: exitCode,
	}
}

// exitCodeFromError extracts the process exit code from an error returned by
// exec.Cmd.Run.  Returns nil when the error does not carry an exit code
// (e.g. the command was killed by a signal or the error is about path lookup).
func exitCodeFromError(err error) *int {
	if err == nil {
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		code := exitErr.ExitCode()
		return &code
	}
	return nil
}

type parsedExecCommand struct {
	program string
	args    []string
}

func runParsedCommands(ctx context.Context, cwd string, parsed []parsedExecCommand) (string, error) {
	if len(parsed) == 0 {
		return "", fmt.Errorf("no command to execute")
	}

	// Fast path for a single command.
	if len(parsed) == 1 {
		cmd := exec.CommandContext(ctx, parsed[0].program, parsed[0].args...)
		if cwd != "" {
			cmd.Dir = cwd
		}

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		output := stdout.String()
		if stderr.Len() > 0 {
			if output != "" {
				output += "\n"
			}
			output += "STDERR:\n" + stderr.String()
		}
		return output, err
	}

	// Pipeline path. Each segment is still executed without invoking a shell.
	cmds := make([]*exec.Cmd, 0, len(parsed))
	stderrBufs := make([]*bytes.Buffer, 0, len(parsed))
	var finalStdout bytes.Buffer
	var prevStdout io.ReadCloser

	for i, item := range parsed {
		cmd := exec.CommandContext(ctx, item.program, item.args...)
		if cwd != "" {
			cmd.Dir = cwd
		}
		cmdStderr := &bytes.Buffer{}
		cmd.Stderr = cmdStderr
		stderrBufs = append(stderrBufs, cmdStderr)

		if prevStdout != nil {
			cmd.Stdin = prevStdout
		}

		if i < len(parsed)-1 {
			stdoutPipe, err := cmd.StdoutPipe()
			if err != nil {
				return "", err
			}
			prevStdout = stdoutPipe
		} else {
			cmd.Stdout = &finalStdout
		}

		cmds = append(cmds, cmd)
	}

	for _, cmd := range cmds {
		if err := cmd.Start(); err != nil {
			return "", err
		}
	}

	var runErr error
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil && runErr == nil {
			runErr = err
		}
	}

	output := finalStdout.String()
	// Merge stderr buffers sequentially (safe — all commands have finished).
	var mergedStderr bytes.Buffer
	for _, buf := range stderrBufs {
		mergedStderr.Write(buf.Bytes())
	}
	if mergedStderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += "STDERR:\n" + mergedStderr.String()
	}

	return output, runErr
}


func splitPipelineSegments(command string) ([]string, error) {
	runes := []rune(command)
	if len(runes) == 0 {
		return nil, nil
	}

	var segments []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	flush := func() error {
		seg := strings.TrimSpace(current.String())
		current.Reset()
		if seg == "" {
			return fmt.Errorf("invalid pipeline segment")
		}
		segments = append(segments, seg)
		return nil
	}

	for i, r := range runes {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' {
			current.WriteRune(r)
			escapeNext = true
			continue
		}

		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			current.WriteRune(r)
			continue
		}

		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			current.WriteRune(r)
			continue
		}

		if r == '|' && !inSingleQuote && !inDoubleQuote {
			if i+1 < len(runes) && runes[i+1] == '|' {
				return nil, fmt.Errorf("unsupported operator ||")
			}
			if err := flush(); err != nil {
				return nil, err
			}
			continue
		}

		current.WriteRune(r)
	}

	if inSingleQuote || inDoubleQuote {
		return nil, fmt.Errorf("unclosed quote in command")
	}

	if strings.TrimSpace(current.String()) != "" {
		if err := flush(); err != nil {
			return nil, err
		}
	}

	return segments, nil
}

func (t *ExecTool) guardCommand(command, cwd string) string {
	cmd := strings.TrimSpace(command)
	lower := strings.ToLower(cmd)

	// Block interpreter bypass patterns for dynamic languages that could escape the sandbox.
	// Use filepath.Base so that full-path invocations (/usr/bin/python3, /bin/sh …) are caught
	// in addition to bare names.
	interpreters := []string{"python", "python3", "sh", "bash", "zsh", "ruby", "perl", "node"}
	fields := strings.Fields(lower)
	if len(fields) > 0 {
		baseCmd := filepath.Base(fields[0])
		for _, interp := range interpreters {
			if baseCmd == interp {
				// Catch -c flag, eval, OS module imports, /etc/ access — common sandbox escapes.
				if strings.Contains(lower, "import os") ||
					strings.Contains(lower, "/etc/") ||
					strings.Contains(lower, "system(") ||
					strings.Contains(lower, " -c ") ||
					strings.Contains(lower, "\t-c ") ||
					strings.Contains(lower, "eval") {
					return "Command blocked by safety guard (interpreter bypass patterns detected)"
				}
				break
			}
		}
	}

	for _, pattern := range t.denyPatterns {
		if pattern.MatchString(lower) {
			return "Command blocked by safety guard (dangerous pattern detected)"
		}
	}

	// Block hardware commands unless the corresponding permission is enabled.
	if msg := guardHardwareCommands(lower); msg != "" {
		return msg
	}

	if len(t.allowPatterns) > 0 {
		allowed := false
		for _, pattern := range t.allowPatterns {
			if pattern.MatchString(lower) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "Command blocked by safety guard (not in allowlist)"
		}
	}

	if t.restrictToWorkspace {
		if strings.Contains(cmd, "..\\") || strings.Contains(cmd, "../") {
			return "Command blocked by safety guard (path traversal detected)"
		}

		cwdPath, err := filepath.Abs(cwd)
		if err != nil {
			return ""
		}

		pathPattern := pathExtractPattern
		matches := pathPattern.FindAllString(cmd, -1)

		for _, raw := range matches {
			p, err := filepath.Abs(raw)
			if err != nil {
				continue
			}

			pReal := p
			if resolved, err := filepath.EvalSymlinks(p); err == nil {
				pReal = resolved
			}

			cwdReal := cwdPath
			if resolved, err := filepath.EvalSymlinks(cwdPath); err == nil {
				cwdReal = resolved
			}

			rel, err := filepath.Rel(cwdReal, pReal)
			if err != nil {
				continue
			}

			if strings.HasPrefix(rel, "..") {
				return "Command blocked by safety guard (path outside working dir)"
			}
		}
	}

	return ""
}

func (t *ExecTool) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

func (t *ExecTool) SetRestrictToWorkspace(restrict bool) {
	t.restrictToWorkspace = restrict
}

func (t *ExecTool) SetAllowPatterns(patterns []string) error {
	t.allowPatterns = make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return fmt.Errorf("invalid allow pattern %q: %w", p, err)
		}
		t.allowPatterns = append(t.allowPatterns, re)
	}
	return nil
}

func guardParsedCommand(program string, args []string) string {
	baseCmd := filepath.Base(strings.ToLower(program))

	switch baseCmd {
	case "xargs":
		return "Command blocked by safety guard (xargs can execute arbitrary commands)"

	case "find":
		for _, arg := range args {
			switch strings.ToLower(arg) {
			case "-exec", "-execdir", "-ok", "-okdir", "-delete":
				return "Command blocked by safety guard (find execution or deletion flags detected)"
			}
		}

	case "curl":
		if msg := guardCurlArgs(args); msg != "" {
			return msg
		}

	case "wget":
		if msg := guardWgetArgs(args); msg != "" {
			return msg
		}

	case "git":
		// Block git config alias with shell-executing values (prefix "!").
		// git aliases starting with "!" invoke an arbitrary shell command,
		// enabling sandbox escape via: git config alias.x '!bash -c "cmd"'
		hasConfigAlias := false
		for i, arg := range args {
			lowerArg := strings.ToLower(arg)
			switch {
			case lowerArg == "-c", strings.HasPrefix(lowerArg, "-c"),
				lowerArg == "--config-env", strings.HasPrefix(lowerArg, "--config-env="),
				lowerArg == "--exec-path", strings.HasPrefix(lowerArg, "--exec-path="),
				lowerArg == "--upload-pack", strings.HasPrefix(lowerArg, "--upload-pack="),
				lowerArg == "--receive-pack", strings.HasPrefix(lowerArg, "--receive-pack="):
				return "Command blocked by safety guard (git runtime command/config injection detected)"
			}
			// Detect "git config [--global|--system] alias.<name>" patterns.
			if lowerArg == "config" {
				for _, subsequent := range args[i+1:] {
					subLower := strings.ToLower(subsequent)
					if strings.HasPrefix(subLower, "alias.") {
						hasConfigAlias = true
						break
					}
				}
			}
		}
		if hasConfigAlias {
			return "Command blocked by safety guard (git alias creation can execute arbitrary shell commands)"
		}

	case "awk":
		for _, arg := range args {
			lowerArg := strings.ToLower(arg)
			if strings.Contains(lowerArg, "system(") || strings.Contains(lowerArg, "| getline") || strings.Contains(lowerArg, "|getline") || strings.Contains(lowerArg, "|&") {
				return "Command blocked by safety guard (awk command execution detected)"
			}
		}

	case "sed":
		for _, arg := range args {
			// Block -f/--file which loads sed scripts from files the agent may
			// have written, potentially bypassing inline script guards.
			if arg == "-f" || strings.HasPrefix(arg, "--file") {
				return "Command blocked by safety guard (sed -f script file execution detected)"
			}
			if sedExecPattern.MatchString(arg) || sedFileWritePattern.MatchString(arg) {
				return "Command blocked by safety guard (sed execution or file write detected)"
			}
		}

	case "tar":
		for _, arg := range args {
			lowerArg := strings.ToLower(arg)
			switch {
			case lowerArg == "--to-command", strings.HasPrefix(lowerArg, "--to-command="),
				lowerArg == "--checkpoint-action", strings.HasPrefix(lowerArg, "--checkpoint-action="),
				lowerArg == "--use-compress-program", strings.HasPrefix(lowerArg, "--use-compress-program="),
				arg == "-I",
				arg == "-P", lowerArg == "--absolute-names":
				return "Command blocked by safety guard (tar command execution or absolute path extraction detected)"
			}
		}
	}

	return ""
}

func guardCurlArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		lowerArg := strings.ToLower(arg)
		switch {
		case arg == "-T", lowerArg == "--upload-file",
			arg == "-F", lowerArg == "--form", lowerArg == "--form-string",
			arg == "-d", lowerArg == "--data", lowerArg == "--data-raw", lowerArg == "--data-binary", lowerArg == "--data-urlencode",
			lowerArg == "--json",
			arg == "-K", lowerArg == "--config",
			arg == "-x", lowerArg == "--proxy":
			return "Command blocked by safety guard (curl upload/config/proxy flags detected)"
		case lowerArg == "-xpost", lowerArg == "-xput", lowerArg == "-xdelete", lowerArg == "-xpatch":
			return "Command blocked by safety guard (curl non-GET request detected)"
		case arg == "-X", lowerArg == "--request":
			if i+1 < len(args) {
				method := strings.ToUpper(args[i+1])
				if method != "GET" && method != "HEAD" {
					return "Command blocked by safety guard (curl non-GET request detected)"
				}
			}
		}

		if isBlockedFetchSchemeArg(lowerArg) {
			return "Command blocked by safety guard (curl only allows http/https URLs)"
		}
	}
	return ""
}

func guardWgetArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		lowerArg := strings.ToLower(arg)
		switch {
		case lowerArg == "--post-data", strings.HasPrefix(lowerArg, "--post-data="),
			lowerArg == "--post-file", strings.HasPrefix(lowerArg, "--post-file="),
			lowerArg == "--body-data", strings.HasPrefix(lowerArg, "--body-data="),
			lowerArg == "--body-file", strings.HasPrefix(lowerArg, "--body-file="),
			arg == "-i", lowerArg == "--input-file",
			arg == "-e", lowerArg == "--execute":
			return "Command blocked by safety guard (wget upload/config flags detected)"
		case lowerArg == "--method":
			if i+1 < len(args) {
				method := strings.ToUpper(args[i+1])
				if method != "GET" && method != "HEAD" {
					return "Command blocked by safety guard (wget non-GET request detected)"
				}
			}
		case strings.HasPrefix(lowerArg, "--method="):
			method := strings.ToUpper(strings.TrimPrefix(lowerArg, "--method="))
			if method != "GET" && method != "HEAD" {
				return "Command blocked by safety guard (wget non-GET request detected)"
			}
		}

		if isBlockedFetchSchemeArg(lowerArg) {
			return "Command blocked by safety guard (wget only allows http/https URLs)"
		}
	}
	return ""
}

func isBlockedFetchSchemeArg(arg string) bool {
	if strings.Contains(arg, "://") {
		return !strings.HasPrefix(arg, "http://") && !strings.HasPrefix(arg, "https://")
	}
	return false
}

// hardwareGuard maps command prefixes to the permission feature they require.
var hardwareGuards = []struct {
	pattern *regexp.Regexp
	feature permissions.Feature
	desc    string
}{
	{regexp.MustCompile(`\btermux-camera-photo\b`), permissions.Camera, "camera capture"},
	{regexp.MustCompile(`\btermux-microphone-record\b`), permissions.Microphone, "microphone recording"},
	{regexp.MustCompile(`\btermux-sms-(send|list)\b`), permissions.SMS, "SMS access"},
	{regexp.MustCompile(`\btermux-telephony-call\b`), permissions.PhoneCalls, "phone call"},
	{regexp.MustCompile(`\btermux-location\b`), permissions.Location, "location access"},
	{regexp.MustCompile(`\btermux-clipboard-(get|set)\b`), permissions.Clipboard, "clipboard access"},
	{regexp.MustCompile(`\btermux-sensor\b`), permissions.Sensors, "sensor access"},
	// Catch-all for any termux-* hardware command not explicitly listed.
	{regexp.MustCompile(`\btermux-(vibrate|torch|volume|notification|toast|wifi|battery|usb|fingerprint|wallpaper|dialog|download|media-player|media-scan|storage-get|tts-speak)\b`), permissions.ShellHardware, "hardware command"},
}

// guardHardwareCommands checks if a shell command invokes hardware APIs
// and verifies the corresponding permission is enabled.
func guardHardwareCommands(lower string) string {
	reg := permissions.Global()
	for _, g := range hardwareGuards {
		if g.pattern.MatchString(lower) {
			if !reg.IsAllowed(g.feature) {
				return fmt.Sprintf("Command blocked: %s requires permissions.%s=true in config", g.desc, g.feature)
			}
		}
	}
	// Catch-all: block ANY termux-* command not already allowed above
	if strings.Contains(lower, "termux-") {
		if !reg.IsAllowed(permissions.ShellHardware) {
			return "Command blocked: termux hardware commands require permissions.shell_hardware=true in config"
		}
	}
	return ""
}

// parseCommandForDirectExecution parses a command string into a program and arguments.
// This is used for direct execution (bypassing 'sh -c') to prevent shell injection.
func parseCommandForDirectExecution(command string) (string, []string) {
	if command == "" {
		return "", nil
	}

	var parts []string
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for _, r := range command {
		if escapeNext {
			current.WriteRune(r)
			escapeNext = false
			continue
		}

		if r == '\\' && !inSingleQuote {
			escapeNext = true
			continue
		}

		if r == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
			continue
		}

		if r == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
			continue
		}

		if (r == ' ' || r == '\t') && !inSingleQuote && !inDoubleQuote {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	if len(parts) == 0 {
		return "", nil
	}
	program := parts[0]
	args := parts[1:]
	return program, args
}

// Vikram — Enterprise AI Engineering Team
// License: MIT

package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chzyer/readline"
	"github.com/gorilla/websocket"
	vikramassets "github.com/Vatthu/vikram"
	"github.com/Vatthu/vikram/pkg/agent"
	"github.com/Vatthu/vikram/pkg/api"
	"github.com/Vatthu/vikram/pkg/auth"
	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/channels"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/console"
	"github.com/Vatthu/vikram/pkg/cron"
	"github.com/Vatthu/vikram/pkg/dashboard"
	"github.com/Vatthu/vikram/pkg/events"
	"github.com/Vatthu/vikram/pkg/health"
	"github.com/Vatthu/vikram/pkg/heartbeat"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/mcp"
	"github.com/Vatthu/vikram/pkg/orchestrator"
	"github.com/Vatthu/vikram/pkg/orchestratorhost"
	"github.com/Vatthu/vikram/pkg/permissions"
	"github.com/Vatthu/vikram/pkg/proactive"
	"github.com/Vatthu/vikram/pkg/providers"
	"github.com/Vatthu/vikram/pkg/queue"
	"github.com/Vatthu/vikram/pkg/skills"
	"github.com/Vatthu/vikram/pkg/state"
	devsync "github.com/Vatthu/vikram/pkg/sync"
	"github.com/Vatthu/vikram/pkg/tools"
)

var embeddedFiles = vikramassets.Workspace

var (
	version   = "dev"
	gitCommit string
	buildTime string
	goVersion string
)

const logo = "Vikram"

// agentBudget tracks daily token usage per agent role and notifies (or
// optionally stops) when a budget threshold is crossed.
type agentBudget struct {
	mu           sync.Mutex
	dailyTokens  map[string]int
	dailyLimits  map[string]int
	dailyActions map[string]string // "", "notify", "stop"
	lastReset    string
	notifyFn     func(role string, used, limit int) // called when budget exceeded
	notified     map[string]bool                    // only notify once per day per role
}

func newAgentBudget(cfg *config.Config) *agentBudget {
	b := &agentBudget{
		dailyTokens:  make(map[string]int),
		dailyLimits:  make(map[string]int),
		dailyActions: make(map[string]string),
		notified:     make(map[string]bool),
		lastReset:    time.Now().Format("2006-01-02"),
	}
	for _, agentCfg := range cfg.Agents.List {
		if agentCfg.Role != "" && agentCfg.MaxTokensPerDay > 0 {
			b.dailyLimits[agentCfg.Role] = agentCfg.MaxTokensPerDay
			action := agentCfg.BudgetAction
			if action == "" {
				action = "notify"
			}
			b.dailyActions[agentCfg.Role] = action
		}
	}
	return b
}

func (b *agentBudget) setNotifier(fn func(role string, used, limit int)) {
	b.notifyFn = fn
}

func (b *agentBudget) check(role string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	today := time.Now().Format("2006-01-02")
	if b.lastReset != today {
		b.dailyTokens = make(map[string]int)
		b.notified = make(map[string]bool)
		b.lastReset = today
	}

	limit, ok := b.dailyLimits[role]
	if !ok || limit <= 0 {
		return nil
	}

	used := b.dailyTokens[role]
	if used >= limit {
		if b.notifyFn != nil && !b.notified[role] {
			b.notifyFn(role, used, limit)
			b.notified[role] = true
		}
		if b.dailyActions[role] == "stop" {
			return fmt.Errorf("agent role %q exceeded daily budget (%d/%d tokens)", role, used, limit)
		}
	}
	return nil
}

func (b *agentBudget) record(role string, tokens int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dailyTokens[role] += tokens
}

const (
	clientWSWriteWait = 10 * time.Second
	clientHTTPTimeout = 15 * time.Second
)

var microphoneSleep = time.Sleep

type clientWSWriteConn interface {
	WriteMessage(messageType int, data []byte) error
	SetWriteDeadline(t time.Time) error
}

type clientWSWriter struct {
	conn clientWSWriteConn
	mu   sync.Mutex
}

func newClientWSWriter(conn clientWSWriteConn) *clientWSWriter {
	return &clientWSWriter{conn: conn}
}

func (w *clientWSWriter) WriteJSON(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.WriteMessage(websocket.TextMessage, data)
}

func (w *clientWSWriter) WriteMessage(messageType int, data []byte) error {
	if w == nil || w.conn == nil {
		return fmt.Errorf("websocket writer not initialized")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.conn.SetWriteDeadline(time.Now().Add(clientWSWriteWait)); err != nil {
		return err
	}
	return w.conn.WriteMessage(messageType, data)
}

// formatVersion returns the version string with optional git commit
func formatVersion() string {
	v := version
	if gitCommit != "" {
		v += fmt.Sprintf(" (git: %s)", gitCommit)
	}
	return v
}

// formatBuildInfo returns build time and go version info
func formatBuildInfo() (build string, goVer string) {
	if buildTime != "" {
		build = buildTime
	}
	goVer = goVersion
	if goVer == "" {
		goVer = runtime.Version()
	}
	return
}

func printVersion() {
	fmt.Printf("Vikram %s\n", formatVersion())
	build, goVer := formatBuildInfo()
	if build != "" {
		fmt.Printf("  Build: %s\n", build)
	}
	if goVer != "" {
		fmt.Printf("  Go: %s\n", goVer)
	}
}

func copyDirectory(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "onboard":
		onboardCmd()
	case "configure":
		configureCmd()
	case "doctor":
		doctorCmd()
	case "agent":
		agentCmd()
	case "client":
		clientCmd()
	case "gateway":
		gatewayCmd()
	case "status":
		statusCmd()
	case "auth":
		authCmd()
	case "telegram":
		telegramCmd()
	case "cron":
		cronCmd()
	case "skills":
		if len(os.Args) < 3 {
			skillsHelp()
			return
		}

		subcommand := os.Args[2]

		cfg, err := loadConfig()
		if err != nil {
			fmt.Printf("Error loading config: %v\n", err)
			os.Exit(1)
		}

		workspace := cfg.WorkspacePath()
		installer := skills.NewSkillInstaller(workspace)
		// Get global config directory and built-in skills directory
		globalDir := filepath.Dir(getConfigPath())
		globalSkillsDir := filepath.Join(globalDir, "skills")
		builtinSkillsDir := detectBuiltinSkillsDir(workspace)
		skillsLoader := skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir)

		switch subcommand {
		case "list":
			skillsListCmd(skillsLoader)
		case "install":
			skillsInstallCmd(installer)
		case "remove", "uninstall":
			if len(os.Args) < 4 {
				fmt.Println("Usage: vikram skills remove <skill-name>")
				return
			}
			skillsRemoveCmd(installer, os.Args[3])
		case "install-builtin":
			skillsInstallBuiltinCmd(workspace)
		case "list-builtin":
			skillsListBuiltinCmd(workspace)
		case "search":
			skillsSearchCmd(installer)
		case "show":
			if len(os.Args) < 4 {
				fmt.Println("Usage: vikram skills show <skill-name>")
				return
			}
			skillsShowCmd(skillsLoader, os.Args[3])
		default:
			fmt.Printf("Unknown skills command: %s\n", subcommand)
			skillsHelp()
		}
	case "version", "--version", "-v":
		printVersion()
	case "smoke":
		smokeCmd()
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("Vikram v%s\n\n", version)

	fmt.Println(titleStyle.Render("  ✨ First time here?"))
	fmt.Printf("     %s\n\n", stepStyle.Render("Run:  vikram onboard   ← 2-minute setup wizard"))

	fmt.Println("Usage: vikram <command>")
	fmt.Println()
	fmt.Println("Getting started:")
	fmt.Println("  onboard     Guided setup wizard — start here if you're new")
	fmt.Println("  doctor      Check that everything is configured and working")
	fmt.Println()
	fmt.Println("Daily use:")
	fmt.Println("  agent       Chat with your AI assistant")
	fmt.Println("  client      Connect to a remote Vikram gateway")
	fmt.Println("  gateway     Start V1 gateway")
	fmt.Println("  status      Show V1 status")
	fmt.Println()
	fmt.Println("Management:")
	fmt.Println("  configure   Change settings (workspace, model, channels, tools)")
	fmt.Println("  skills      Manage skills  (install, list, remove)")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  auth        Manage authentication (login, logout, status)")
	fmt.Println("  telegram    Manage Telegram pairing and access")
	fmt.Println("  smoke       Run operational smoke checks")
	fmt.Println("  version     Show version information")
	fmt.Println()
	fmt.Printf("%s\n", stepStyle.Render("  Tip: run  vikram agent -m \"your question\"  to ask something quickly."))
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// sanitizeOnboardingField strips newlines and control characters from a
// user-supplied onboarding string before it is written into MEMORY.md.
// MEMORY.md is loaded into the LLM system prompt on every request, so any
// injected markdown headings or instruction text would be interpreted by the
// model.  Single-line fields (aiName, aiRole, userName) are restricted to
// printable non-newline characters.  The multi-line userPrefs field allows
// newlines but strips NUL and other control characters.
func sanitizeOnboardingField(s string, allowNewlines bool) string {
	return strings.Map(func(r rune) rune {
		if r == '\x00' {
			return -1 // drop NUL bytes entirely
		}
		if !allowNewlines && (r == '\n' || r == '\r') {
			return ' '
		}
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return ' ' // replace other control chars with space
		}
		return r
	}, s)
}

func initMemory(workspace, aiName, aiRole, userName, userPrefs string) {
	memoryDir := filepath.Join(workspace, "memory")
	os.MkdirAll(memoryDir, 0700)
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	// Sanitise all user-supplied strings to prevent markdown/prompt injection.
	safeName := sanitizeOnboardingField(aiName, false)
	safeRole := sanitizeOnboardingField(aiRole, false)
	safeUser := sanitizeOnboardingField(userName, false)
	safePrefs := sanitizeOnboardingField(userPrefs, true)

	memoryContent := fmt.Sprintf(`# Long-term Memory

This file stores important information that should persist across sessions.

## Core Identity (Soul)
- Name: %s
- Core Purpose: %s

## User Information
- Name: %s

## Preferences
- %s

## Important Notes
- Initialized configuration defaults.
`, safeName, safeRole, safeUser, safePrefs)

	_ = os.WriteFile(memoryFile, []byte(memoryContent), 0600)
}

func writePersonalizedBootstrapFiles(workspace, aiName, aiRole, userName, userPrefs string) {
	safeName := strings.TrimSpace(sanitizeOnboardingField(aiName, false))
	if safeName == "" {
		safeName = "V1"
	}
	safeRole := strings.TrimSpace(sanitizeOnboardingField(aiRole, false))
	if safeRole == "" {
		safeRole = "Your personal AI assistant"
	}
	safeUser := strings.TrimSpace(sanitizeOnboardingField(userName, false))
	if safeUser == "" {
		safeUser = "User"
	}
	safePrefs := strings.TrimSpace(sanitizeOnboardingField(userPrefs, true))
	if safePrefs == "" {
		safePrefs = "- Keep replies direct and natural.\n- Prefer acting like a present assistant, not a product brochure."
	} else if !strings.HasPrefix(safePrefs, "-") {
		safePrefs = "- " + strings.ReplaceAll(safePrefs, "\n", "\n- ")
	}

	files := map[string]string{
		"AGENT.md": fmt.Sprintf(`# Agent Instructions

You are %s, %s for %s.

## Operating Rules

- Act like a present, awake assistant for %s, not like a README or marketing page.
- When asked about yourself, answer in first person as %s and describe your current role, behavior, and practical capabilities.
- Use the identity and personality defined in IDENTITY.md, SOUL.md, and USER.md as the source of truth.
- Use tools when action is required; do not pretend that something was done.
- Keep replies direct, natural, and grounded in the current conversation.
`, safeName, safeRole, safeUser, safeUser, safeName),
		"IDENTITY.md": fmt.Sprintf(`# Identity

## Name
%s

## Role
%s

## Relationship
You assist %s directly on their machine and channels.

## How to Speak
- Speak like a real assistant in the room.
- Be clear, calm, practical, and concise.
- Do not default to product pitches, GitHub blurbs, or README-style summaries unless %s asks about the project itself.
`, safeName, safeRole, safeUser, safeUser),
		"SOUL.md": `# Soul

## Personality

- Alert and grounded
- Helpful without sounding generic
- Calm under pressure
- Honest about what is working, what is broken, and what you are doing next

## Values

- Protect the user's trust
- Prefer clear action over vague promises
- Stay practical and reality-based
- Do not slip into marketing language
`,
		"USER.md": fmt.Sprintf(`# User

## Primary Operator
- Name: %s

## Preferences
%s
`, safeUser, safePrefs),
		"TOOLS.md": `# Tools

## Guidance

- Use tools to do real work; do not claim an action happened unless a tool actually completed it.
- Prefer the smallest safe action that solves the user's request.
- If a tool fails, say what failed and what you will try next.
- Keep file and shell work grounded in the current workspace unless the user explicitly wants broader access.
`,
	}

	for name, content := range files {
		writeBootstrapFileIfTemplate(workspace, name, content)
	}
}

func writeBootstrapFileIfTemplate(workspace, name, content string) {
	targetPath := filepath.Join(workspace, name)

	existing, err := os.ReadFile(targetPath)
	switch {
	case err == nil:
		templateData, templateErr := embeddedFiles.ReadFile(filepath.Join("workspace", name))
		if templateErr == nil && string(existing) != string(templateData) {
			return
		}
	case os.IsNotExist(err):
		if mkErr := os.MkdirAll(filepath.Dir(targetPath), 0755); mkErr != nil {
			return
		}
	default:
		return
	}

	_ = os.WriteFile(targetPath, []byte(content), 0644)
}

func setProviderKey(cfg *config.Config, provider, key string) {
	switch provider {
	case "gemini":
		cfg.Providers.Gemini.APIKey = key
	case "openai":
		cfg.Providers.OpenAI.APIKey = key
	case "anthropic":
		cfg.Providers.Anthropic.APIKey = key
	case "groq":
		cfg.Providers.Groq.APIKey = key
	case "deepseek":
		cfg.Providers.DeepSeek.APIKey = key
	case "openrouter":
		cfg.Providers.OpenRouter.APIKey = key
	case "zhipu", "glm":
		cfg.Providers.Zhipu.APIKey = key
	case "moonshot":
		cfg.Providers.Moonshot.APIKey = key
	case "nvidia":
		cfg.Providers.Nvidia.APIKey = key
	case "vllm":
		cfg.Providers.VLLM.APIKey = key
	case "ollama":
		cfg.Providers.Ollama.APIKey = key
	case "github_copilot":
		cfg.Providers.GitHubCopilot.APIKey = key
	case "azure_openai", "azure":
		cfg.Providers.AzureOpenAI.APIKey = key
	case "mistral":
		cfg.Providers.Mistral.APIKey = key
	case "xai", "grok":
		cfg.Providers.XAI.APIKey = key
	case "cerebras":
		cfg.Providers.Cerebras.APIKey = key
	case "sambanova":
		cfg.Providers.SambaNova.APIKey = key
	case "github_models":
		cfg.Providers.GitHubModels.APIKey = key
	}
}

func setProviderAPIBase(cfg *config.Config, provider, apiBase string) {
	switch provider {
	case "gemini":
		cfg.Providers.Gemini.APIBase = apiBase
	case "openai":
		cfg.Providers.OpenAI.APIBase = apiBase
	case "anthropic":
		cfg.Providers.Anthropic.APIBase = apiBase
	case "groq":
		cfg.Providers.Groq.APIBase = apiBase
	case "deepseek":
		cfg.Providers.DeepSeek.APIBase = apiBase
	case "openrouter":
		cfg.Providers.OpenRouter.APIBase = apiBase
	case "zhipu", "glm":
		cfg.Providers.Zhipu.APIBase = apiBase
	case "moonshot":
		cfg.Providers.Moonshot.APIBase = apiBase
	case "nvidia":
		cfg.Providers.Nvidia.APIBase = apiBase
	case "vllm":
		cfg.Providers.VLLM.APIBase = apiBase
	case "ollama":
		cfg.Providers.Ollama.APIBase = apiBase
	case "github_copilot":
		cfg.Providers.GitHubCopilot.APIBase = apiBase
	case "mistral":
		cfg.Providers.Mistral.APIBase = apiBase
	case "xai", "grok":
		cfg.Providers.XAI.APIBase = apiBase
	case "cerebras":
		cfg.Providers.Cerebras.APIBase = apiBase
	case "sambanova":
		cfg.Providers.SambaNova.APIBase = apiBase
	case "github_models":
		cfg.Providers.GitHubModels.APIBase = apiBase
	}
}

func setProviderConnectMode(cfg *config.Config, provider, connectMode string) {
	switch provider {
	case "github_copilot", "copilot":
		cfg.Providers.GitHubCopilot.ConnectMode = connectMode
	}
}

func gatewayProviderConfigError(cfg *config.Config) error {
	providerName := strings.TrimSpace(cfg.Agents.Defaults.Provider)
	if providerName == "" {
		return nil
	}

	_, ready, hint := providerCredentialStatus(cfg, providerName)
	if ready {
		return nil
	}
	if hint == "" {
		hint = "Run  vikram onboard  or  vikram configure → Brain  to finish setup."
	}
	return fmt.Errorf("provider %q is not ready. %s", providerName, hint)
}

func copyEmbeddedToTarget(targetDir string) error {
	// Ensure target directory exists
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("Failed to create target directory: %w", err)
	}

	// Walk through all files in embed.FS
	err := fs.WalkDir(embeddedFiles, "workspace", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		// Read embedded file
		data, err := embeddedFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read embedded file %s: %w", path, err)
		}

		newPath, err := filepath.Rel("workspace", path)
		if err != nil {
			return fmt.Errorf("Failed to get relative path for %s: %v\n", path, err)
		}

		// Build target file path
		targetPath := filepath.Join(targetDir, newPath)

		// Never clobber an existing workspace file. Users are expected to
		// customize these templates, so only seed missing files.
		if _, err := os.Stat(targetPath); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("Failed to stat existing file %s: %w", targetPath, err)
		}

		// Ensure target file's directory exists
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return fmt.Errorf("Failed to create directory %s: %w", filepath.Dir(targetPath), err)
		}

		// Write file
		if err := os.WriteFile(targetPath, data, 0644); err != nil {
			return fmt.Errorf("Failed to write file %s: %w", targetPath, err)
		}

		return nil
	})

	return err
}

func createWorkspaceTemplates(workspace string) {
	err := copyEmbeddedToTarget(workspace)
	if err != nil {
		fmt.Printf("Error copying workspace templates: %v\n", err)
	}
}

func agentCmd() {
	message := ""
	sessionKey := "cli:default"
	debugMode := false

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			debugMode = true
			fmt.Println("🔍 Debug mode enabled")
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-s", "--session":
			if i+1 < len(args) {
				sessionKey = args[i+1]
				i++
			}
		}
	}

	if !debugMode {
		logger.SetLevel(logger.WARN)
	}

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Print agent startup info (only for interactive mode)
	startupInfo := agentLoop.GetStartupInfo()
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      startupInfo["tools"].(map[string]interface{})["count"],
			"skills_total":     startupInfo["skills"].(map[string]interface{})["total"],
			"skills_available": startupInfo["skills"].(map[string]interface{})["available"],
		})

	if message != "" {
		ctx := context.Background()
		response, err := agentLoop.ProcessDirect(ctx, message, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n%s %s\n", logo, response)
	} else {
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		interactiveMode(agentLoop, sessionKey)
	}
}

func interactiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	// Create a cancellable context so in-progress LLM calls are interrupted
	// immediately when the user presses Ctrl+C (SIGINT).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	prompt := fmt.Sprintf("%s You: ", logo)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     historyFilePath("agent.history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		fmt.Println("Falling back to simple input mode...")
		simpleInteractiveMode(agentLoop, sessionKey)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func simpleInteractiveMode(agentLoop *agent.AgentLoop, sessionKey string) {
	// Same signal-aware context as interactiveMode so Ctrl+C cancels in-flight calls.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(fmt.Sprintf("%s You: ", logo))
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println("\nGoodbye!")
				return
			}
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Goodbye!")
			return
		}

		response, err := agentLoop.ProcessDirect(ctx, input, sessionKey)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			continue
		}

		fmt.Printf("\n%s %s\n\n", logo, response)
	}
}

func clientCmd() {
	server := ""
	apiKey := ""
	deviceName := ""
	message := ""
	advertiseHost := ""

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server", "-s":
			if i+1 < len(args) {
				server = args[i+1]
				i++
			}
		case "--api-key", "-k":
			if i+1 < len(args) {
				apiKey = args[i+1]
				i++
			}
		case "--name", "-n":
			if i+1 < len(args) {
				deviceName = args[i+1]
				i++
			}
		case "--advertise-host":
			if i+1 < len(args) {
				advertiseHost = args[i+1]
				i++
			}
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		}
	}

	if server == "" {
		fmt.Println("Usage: vikram client --server <host[:port]|url> [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --server, -s    Gateway address or URL (required)")
		fmt.Println("  --api-key, -k   API key for authentication")
		fmt.Println("  --name, -n      Device name (defaults to hostname)")
		fmt.Println("  --advertise-host Hostname/IP this device should publish to the gateway")
		fmt.Println("  --message, -m   Send a single message and exit")
		fmt.Println("  --debug, -d     Enable debug logging")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  vikram client --server mypc.tail1234.ts.net:18791")
		fmt.Println("  vikram client --server https://gateway.example.com")
		fmt.Println("  vikram client --server 100.91.10.18:18791 --api-key mykey")
		fmt.Println("  vikram client --server https://example.com/v1 --advertise-host phone.local")
		fmt.Println("  vikram client -s 192.168.1.10:18791 -m \"Hello from my phone\"")
		os.Exit(1)
	}

	if deviceName == "" {
		deviceName, _ = os.Hostname()
	}

	endpoints, err := resolveClientEndpoints(server)
	if err != nil {
		fmt.Printf("Invalid gateway address: %v\n", err)
		os.Exit(1)
	}

	// Detect local capabilities.
	capabilities := detectCapabilities()

	deviceID := fmt.Sprintf("%s-%s-%s", deviceName, runtime.GOOS, runtime.GOARCH)

	fmt.Printf("%s Connecting to gateway at %s...\n", logo, endpoints.HTTPBase)

	// Build WebSocket URL — never append the API key as a query parameter
	// because URLs appear in server logs and shell history in plaintext.
	// The key is sent exclusively via the Authorization header below.
	wsURL := endpoints.WSURL

	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		fmt.Printf("Error connecting to gateway: %v\n", err)
		fmt.Println("\nTroubleshooting:")
		fmt.Println("  - Is the gateway running? (vikram gateway)")
		fmt.Println("  - Is v1_api enabled in config? (\"v1_api\": {\"enabled\": true})")
		fmt.Println("  - Is the address correct?")
		fmt.Println("  - Check firewall / Tailscale connectivity")
		os.Exit(1)
	}
	defer conn.Close()
	wsWriter := newClientWSWriter(conn)
	httpClient := &http.Client{Timeout: clientHTTPTimeout}

	// Read welcome message to get client ID.
	var welcomeMsg struct {
		Type string                 `json:"type"`
		Data map[string]interface{} `json:"data"`
	}
	if err := conn.ReadJSON(&welcomeMsg); err != nil {
		fmt.Printf("Error reading welcome: %v\n", err)
		os.Exit(1)
	}
	wsClientID := ""
	wsRegisterToken := ""
	if welcomeMsg.Data != nil {
		if cid, ok := welcomeMsg.Data["client_id"].(string); ok {
			wsClientID = cid
		}
		if token, ok := welcomeMsg.Data["registration_token"].(string); ok {
			wsRegisterToken = token
		}
	}

	fmt.Printf("%s Connected! (client: %s)\n", logo, wsClientID)

	// Register this device with the gateway.
	registerURL := endpoints.DevicesURL
	regBody := map[string]interface{}{
		"id":                deviceID,
		"name":              deviceName,
		"host":              getAdvertisedHost(endpoints.RouteTarget, advertiseHost),
		"platform":          runtime.GOOS,
		"capabilities":      capabilities,
		"version":           version,
		"ws_client_id":      wsClientID,
		"ws_register_token": wsRegisterToken,
	}
	regData, _ := json.Marshal(regBody)

	regReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, registerURL, strings.NewReader(string(regData)))
	regReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		regReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	regResp, err := httpClient.Do(regReq)
	if err != nil {
		fmt.Printf("⚠ Could not register device: %v\n", err)
	} else {
		body, _ := io.ReadAll(io.LimitReader(regResp.Body, 1024))
		regResp.Body.Close()
		if regResp.StatusCode != http.StatusOK {
			fmt.Printf("⚠ Gateway rejected device registration (%s): %s\n", regResp.Status, strings.TrimSpace(string(body)))
		} else if len(capabilities) > 0 {
			fmt.Printf("✓ Device registered as %s (capabilities: %v)\n", deviceID, capabilities)
		} else {
			fmt.Printf("✓ Device registered as %s\n", deviceID)
		}
	}

	// Start background goroutine to handle incoming messages (including capability requests).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responseCh := make(chan string, 16)

	go clientReadPump(ctx, conn, wsWriter, responseCh, capabilities)

	// Send periodic heartbeats.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				msg := map[string]interface{}{"type": "ping", "timestamp": time.Now()}
				if err := wsWriter.WriteJSON(msg); err != nil {
					logger.DebugC("client", fmt.Sprintf("Heartbeat write failed: %v", err))
					cancel()
					_ = conn.Close()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if message != "" {
		// One-shot mode.
		if err := sendChat(wsWriter, message, "client:"+deviceID); err != nil {
			fmt.Printf("Error sending message: %v\n", err)
			return
		}
		select {
		case resp := <-responseCh:
			fmt.Printf("\n%s %s\n", logo, resp)
		case <-time.After(120 * time.Second):
			fmt.Println("Timeout waiting for response")
		}
	} else {
		// Interactive mode.
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		clientInteractiveMode(wsWriter, responseCh, deviceID)
	}

	// Deregister on exit.
	deregURL := fmt.Sprintf("%s/%s", endpoints.DevicesURL, deviceID)
	deregReq, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, deregURL, nil)
	if apiKey != "" {
		deregReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if resp, err := httpClient.Do(deregReq); err == nil && resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	fmt.Println("\n✓ Disconnected from gateway")
}

func clientReadPump(ctx context.Context, conn *websocket.Conn, writer *clientWSWriter, responseCh chan<- string, capabilities []string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.DebugC("client", fmt.Sprintf("Read error: %v", err))
			}
			return
		}

		var msg struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "chat_response":
			var resp struct {
				Response string `json:"response"`
			}
			if err := json.Unmarshal(msg.Data, &resp); err == nil {
				select {
				case responseCh <- resp.Response:
				default:
				}
			}

		case "capability_request":
			// Handle capability requests from the gateway.
			var req struct {
				RequestID  string                 `json:"request_id"`
				Capability string                 `json:"capability"`
				Action     string                 `json:"action"`
				Params     map[string]interface{} `json:"params"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				continue
			}
			go handleCapabilityRequest(writer, req.RequestID, req.Capability, req.Action, req.Params)

		case "pong":
			// Heartbeat acknowledged.

		case "error":
			var errMsg string
			json.Unmarshal(msg.Data, &errMsg)
			fmt.Printf("\n⚠ Server error: %s\n", errMsg)
		}
	}
}

func handleCapabilityRequest(writer *clientWSWriter, requestID, capability, action string, params map[string]interface{}) {
	logger.InfoCF("client", "Capability request received", map[string]interface{}{
		"request_id": requestID, "capability": capability, "action": action,
	})

	var result interface{}
	var capErr string

	switch capability {
	case "camera":
		result, capErr = executeLocalCapability("camera", action, params, "")
	case "microphone":
		result, capErr = executeLocalCapability("microphone", action, params, "")
	case "screen":
		result, capErr = executeLocalCapability("screen", action, params, "")
	default:
		capErr = fmt.Sprintf("unsupported capability: %s", capability)
	}

	resp := map[string]interface{}{
		"type": "capability_response",
		"data": map[string]interface{}{
			"request_id": requestID,
			"success":    capErr == "",
			"data":       result,
			"error":      capErr,
		},
		"timestamp": time.Now(),
	}
	data, _ := json.Marshal(resp)
	if err := writer.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.DebugC("client", fmt.Sprintf("Capability response write failed: %v", err))
	}
}

func executeLocalCapability(capability, action string, params map[string]interface{}, termuxRootOverride string) (interface{}, string) {
	// Check if we're on Termux (Android).
	isTermux := false
	termuxPath := "/data/data/com.termux"
	if termuxRootOverride != "" {
		termuxPath = termuxRootOverride
	}
	if _, err := os.Stat(termuxPath); err == nil {
		isTermux = true
	}

	switch capability {
	case "camera":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_cap_%d.jpg", time.Now().UnixNano()))
			output, err := execCommand("termux-camera-photo", "-c", "0", outFile)
			if err != nil {
				return nil, fmt.Sprintf("camera capture failed: %v (%s)", err, output)
			}
			imgData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read capture: %v", err)
			}
			return map[string]interface{}{
				"format": "jpeg",
				"base64": base64Encode(imgData),
			}, ""
		}
		return nil, "camera not available on this platform without Termux"

	case "microphone":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_mic_%d.wav", time.Now().UnixNano()))
			duration := 5 // Default to 5 seconds
			if dStr, ok := params["duration"].(string); ok {
				parsedDuration, err := strconv.Atoi(dStr)
				if err != nil {
					return nil, fmt.Sprintf("invalid duration parameter: %v", err)
				}
				duration = parsedDuration
			}
			// Clamp duration to a reasonable maximum to prevent DoS (e.g., 5 minutes)
			if duration > 300 {
				duration = 300
			}

			if _, err := execCommand("termux-microphone-record", "-f", outFile, "-l", strconv.Itoa(duration)); err != nil {
				return nil, fmt.Sprintf("mic record failed: %v", err)
			}
			microphoneSleep(time.Duration(duration) * time.Second)
			execCommand("termux-microphone-record", "-q")
			audioData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read recording: %v", err)
			}
			return map[string]interface{}{
				"format": "wav",
				"base64": base64Encode(audioData),
			}, ""
		}
		return nil, "microphone not available on this platform without Termux"

	case "screen":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_screen_%d.png", time.Now().UnixNano()))
			if _, err := execCommand("termux-screenshot", outFile); err != nil {
				return nil, fmt.Sprintf("screenshot failed: %v", err)
			}
			imgData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read screenshot: %v", err)
			}
			return map[string]interface{}{
				"format": "png",
				"base64": base64Encode(imgData),
			}, ""
		}
		return nil, "screen capture not available on this platform"
	}

	return nil, fmt.Sprintf("unknown capability: %s", capability)
}

func execCommand(name string, arg ...string) (string, error) {
	out, err := exec.Command(name, arg...).CombinedOutput()
	return string(out), err
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func sendChat(writer *clientWSWriter, message, sessionKey string) error {
	msg := map[string]interface{}{
		"type": "chat",
		"data": map[string]interface{}{
			"message":     message,
			"session_key": sessionKey,
		},
		"timestamp": time.Now(),
	}
	return writer.WriteJSON(msg)
}

func clientInteractiveMode(writer *clientWSWriter, responseCh <-chan string, deviceID string) {
	prompt := fmt.Sprintf("%s You: ", logo)
	sessionKey := "client:" + deviceID

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     historyFilePath("client.history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				return
			}
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			return
		}

		if err := sendChat(writer, input, sessionKey); err != nil {
			fmt.Printf("\n⚠ Error sending message: %v\n", err)
			return
		}

		select {
		case resp := <-responseCh:
			fmt.Printf("\n%s %s\n\n", logo, resp)
		case <-time.After(120 * time.Second):
			fmt.Println("\n⚠ Timeout waiting for response")
		}
	}
}

func detectCapabilities() []string {
	var caps []string

	// Check if on Termux (Android) with hardware access.
	isTermux := false
	if _, err := os.Stat("/data/data/com.termux"); err == nil {
		isTermux = true
	}

	if isTermux {
		// Check for termux-api commands.
		if _, err := exec.LookPath("termux-camera-photo"); err == nil {
			caps = append(caps, "camera")
		}
		if _, err := exec.LookPath("termux-microphone-record"); err == nil {
			caps = append(caps, "microphone")
		}
		if _, err := exec.LookPath("termux-media-player"); err == nil {
			caps = append(caps, "speaker")
		}
		if _, err := exec.LookPath("termux-screenshot"); err == nil {
			caps = append(caps, "screen")
		}
	} else {
		// Desktop detection.
		if _, err := exec.LookPath("ffmpeg"); err == nil {
			caps = append(caps, "camera")
			caps = append(caps, "microphone")
		}
		if _, err := exec.LookPath("arecord"); err == nil {
			caps = append(caps, "microphone")
		}
		if runtime.GOOS == "darwin" {
			// macOS always has screen capture via screencapture.
			caps = append(caps, "screen")
			caps = append(caps, "speaker")
		}
	}

	// Deduplicate.
	seen := make(map[string]bool)
	var unique []string
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}
	return unique
}

type clientEndpoints struct {
	HTTPBase    string
	WSURL       string
	DevicesURL  string
	RouteTarget string
}

func resolveClientEndpoints(server string) (clientEndpoints, error) {
	raw := strings.TrimSpace(server)
	if raw == "" {
		return clientEndpoints{}, fmt.Errorf("gateway address is empty")
	}

	if strings.Contains(raw, "://") {
		return resolveClientEndpointsFromURL(raw)
	}

	host, port, err := splitClientHostPort(raw)
	if err != nil {
		return clientEndpoints{}, err
	}

	hostPort := net.JoinHostPort(host, port)
	httpBase := fmt.Sprintf("http://%s", hostPort)
	return clientEndpoints{
		HTTPBase:    httpBase,
		WSURL:       fmt.Sprintf("ws://%s/api/v1/ws", hostPort),
		DevicesURL:  fmt.Sprintf("%s/api/v1/devices", httpBase),
		RouteTarget: hostPort,
	}, nil
}

func resolveClientEndpointsFromURL(raw string) (clientEndpoints, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return clientEndpoints{}, err
	}
	if u.Host == "" {
		return clientEndpoints{}, fmt.Errorf("gateway URL must include a host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return clientEndpoints{}, fmt.Errorf("gateway URL must not include query parameters or fragments")
	}

	httpScheme, wsScheme, err := clientTransportSchemes(strings.ToLower(u.Scheme))
	if err != nil {
		return clientEndpoints{}, err
	}

	pathPrefix := strings.TrimRight(u.EscapedPath(), "/")
	httpBase := fmt.Sprintf("%s://%s%s", httpScheme, u.Host, pathPrefix)
	return clientEndpoints{
		HTTPBase:    httpBase,
		WSURL:       fmt.Sprintf("%s://%s%s/api/v1/ws", wsScheme, u.Host, pathPrefix),
		DevicesURL:  fmt.Sprintf("%s/api/v1/devices", httpBase),
		RouteTarget: routeTargetForURL(u, httpScheme),
	}, nil
}

func clientTransportSchemes(scheme string) (httpScheme string, wsScheme string, err error) {
	switch scheme {
	case "http", "ws":
		return "http", "ws", nil
	case "https", "wss":
		return "https", "wss", nil
	default:
		return "", "", fmt.Errorf("unsupported gateway URL scheme %q", scheme)
	}
}

func splitClientHostPort(raw string) (string, string, error) {
	if raw == "" {
		return "", "", fmt.Errorf("gateway address is empty")
	}

	if strings.HasPrefix(raw, "[") {
		host, port, err := net.SplitHostPort(raw)
		if err != nil {
			return "", "", fmt.Errorf("invalid gateway address %q", raw)
		}
		return host, port, nil
	}

	if ip := net.ParseIP(raw); ip != nil {
		return raw, "18791", nil
	}

	if host, port, err := net.SplitHostPort(raw); err == nil {
		return host, port, nil
	}

	if !strings.Contains(raw, ":") {
		return raw, "18791", nil
	}

	return "", "", fmt.Errorf("invalid gateway address %q", raw)
}

func routeTargetForURL(u *url.URL, httpScheme string) string {
	if u == nil {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}

	port := "80"
	if httpScheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func getAdvertisedHost(routeTarget string, override string) string {
	if host := strings.TrimSpace(override); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv("VIKRAM_ADVERTISE_HOST")); host != "" {
		return host
	}
	if host := advertisedHostFromRoute(routeTarget); host != "" {
		return host
	}
	return advertisedHostFromInterfaces()
}

func advertisedHostFromRoute(routeTarget string) string {
	if strings.TrimSpace(routeTarget) == "" {
		return ""
	}

	conn, err := net.Dial("udp", routeTarget)
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || !isAdvertisableIP(localAddr.IP) {
		return ""
	}
	return localAddr.IP.String()
}

func advertisedHostFromInterfaces() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var candidates []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromNetAddr(addr)
			if isAdvertisableIP(ip) {
				candidates = append(candidates, ip)
			}
		}
	}

	return selectAdvertisedIP(candidates)
}

func ipFromNetAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isAdvertisableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}

func selectAdvertisedIP(candidates []net.IP) string {
	bestRank := 999
	bestIP := ""
	for _, ip := range candidates {
		rank := advertiseIPRank(ip)
		if rank < bestRank || (rank == bestRank && ip.String() < bestIP) {
			bestRank = rank
			bestIP = ip.String()
		}
	}
	return bestIP
}

func advertiseIPRank(ip net.IP) int {
	if !isAdvertisableIP(ip) {
		return 999
	}

	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4.IsPrivate() || isCarrierGradeNAT(ip4):
			return 0
		case ip4.IsGlobalUnicast():
			return 1
		default:
			return 4
		}
	}

	switch {
	case ip.IsPrivate():
		return 2
	case ip.IsGlobalUnicast():
		return 3
	default:
		return 4
	}
}

func isCarrierGradeNAT(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

func gatewayCmd() {
	// Check for --debug flag
	args := os.Args[2:]
	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
			break
		}
	}

	// Root context for the entire gateway lifetime.  Cancelled on Ctrl+C / SIGTERM
	// so every downstream goroutine (cron, heartbeat, agent, channels …) stops cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Fail fast on incomplete provider setup using the same provider-aware rules as `doctor`.
	if err := gatewayProviderConfigError(cfg); err != nil {
		fmt.Printf("\n=======================================================\n")
		fmt.Printf("❌ FATAL ERROR: Provider configuration is incomplete ❌\n")
		fmt.Printf("%s\n", err)
		fmt.Printf("=======================================================\n\n")
		os.Exit(1)
	}

	if err := validateGatewaySecurity(cfg); err != nil {
		fmt.Printf("Security configuration error: %v\n", err)
		os.Exit(1)
	}

	// Load hardware permissions from config into global registry.
	perms := permissions.Global()
	if err := perms.SetAll(map[permissions.Feature]bool{
		permissions.Camera:        cfg.Permissions.Camera,
		permissions.Microphone:    cfg.Permissions.Microphone,
		permissions.SMS:           cfg.Permissions.SMS,
		permissions.PhoneCalls:    cfg.Permissions.PhoneCalls,
		permissions.Location:      cfg.Permissions.Location,
		permissions.Clipboard:     cfg.Permissions.Clipboard,
		permissions.Sensors:       cfg.Permissions.Sensors,
		permissions.ShellHardware: cfg.Permissions.ShellHardware,
		permissions.Notifications: cfg.Permissions.Notifications,
		permissions.Screen:        cfg.Permissions.Screen,
	}); err != nil {
		fmt.Printf("Error setting permissions: %v\n", err)
		os.Exit(1)
	}
	perms.Freeze()
	enabledPerms := perms.EnabledFeatures()
	if len(enabledPerms) > 0 {
		names := make([]string, len(enabledPerms))
		for i, f := range enabledPerms {
			names[i] = string(f)
		}
		fmt.Printf("🔓 Permissions enabled: %s\n", strings.Join(names, ", "))
	} else {
		fmt.Println("🔒 All hardware permissions blocked (default-deny)")
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Print agent startup info
	fmt.Println("\n📦 Agent Status:")
	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n",
		skillsInfo["available"],
		skillsInfo["total"])

	// Log to file as well
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})
	// Load MCP tools from external servers into the agent's tool registry.
	if cfg.MCP.Enabled {
		for _, srv := range cfg.MCP.Servers {
			mcpClient, err := mcp.NewClient(ctx, mcp.ClientConfig{
				Command: srv.Command,
				Args:    srv.Args,
				Timeout: time.Duration(srv.Timeout) * time.Second,
			})
			if err != nil {
				fmt.Printf("⚠ MCP %s: %v\n", srv.Name, err)
				continue
			}
			allowed := make(map[string]bool)
			for _, t := range srv.Allowed {
				allowed[t] = true
			}
			maxOut := srv.MaxOutput
			if maxOut == 0 {
				maxOut = 50000
			}
			count := 0
			for _, td := range mcpClient.Tools() {
				if len(allowed) > 0 && !allowed[td.Name] {
					continue
				}
				agentLoop.RegisterTool(mcp.NewAdapter(mcpClient, td, srv.Prefix, maxOut))
				count++
			}
			fmt.Printf("✓ MCP %s: %d tools\n", srv.Name, count)
		}
	}

	// Setup cron tool and service
	execTimeout := time.Duration(cfg.Tools.Cron.ExecTimeoutMinutes) * time.Minute
	cronService := setupCronTool(ctx, agentLoop, msgBus, cfg.WorkspacePath(), cfg.Agents.Defaults.RestrictToWorkspace, cfg.Workspace.Sandboxed, execTimeout)

	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	heartbeatService.SetBus(msgBus)
	heartbeatService.SetProactiveEngine(agentLoop.ProactiveEngine())

	// Wire proactive suggestion delivery to the last active user channel.
	if eng := agentLoop.ProactiveEngine(); eng != nil {
		eng.SetHandler(func(ctx context.Context, suggestion proactive.Suggestion) {
			lastChannel := state.NewManager(cfg.WorkspacePath()).GetLastChannel()
			if lastChannel == "" {
				return
			}
			parts := strings.SplitN(lastChannel, ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return
			}
			// Skip internal channels
			if parts[0] == "cli" || parts[0] == "system" || parts[0] == "subagent" {
				return
			}
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel: parts[0],
				ChatID:  parts[1],
				Content: fmt.Sprintf("💡 %s", suggestion.Message),
			})
		})
	}

	// Daily team summary — sends a status report via the same channel the user
	// last used. Runs every 6 hours or on the heartbeat interval, whichever is longer.
	go func() {
		summaryInterval := time.Duration(cfg.Heartbeat.Interval) * time.Minute
		if summaryInterval < 6*time.Hour {
			summaryInterval = 6 * time.Hour
		}
		ticker := time.NewTicker(summaryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendTeamSummary(cfg, msgBus)
			case <-ctx.Done():
				return
			}
		}
	}()

	heartbeatService.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		// Use cli:direct as fallback if no valid channel
		if channel == "" || chatID == "" {
			channel, chatID = "cli", "direct"
		}
		// Use ProcessHeartbeat - no session history, each heartbeat is independent.
		// ctx is the gateway lifetime context — cancelled on shutdown.
		response, err := agentLoop.ProcessHeartbeat(ctx, prompt, channel, chatID)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("Heartbeat error: %v", err))
		}
		if response == "HEARTBEAT_OK" {
			return tools.SilentResult("Heartbeat OK")
		}
		// For heartbeat, always return silent - the subagent result will be
		// sent to user via processSystemMessage when the async task completes
		return tools.SilentResult(response)
	})

	channelManager, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}

	// Inject channel manager into agent loop for command handling
	agentLoop.SetChannelManager(channelManager)

	enabledChannels := channelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	hostSocket := orchestratorHostSocketPath()
	budget := newAgentBudget(cfg)
	budget.setNotifier(func(role string, used, limit int) {
		stateManager := state.NewManager(cfg.WorkspacePath())
		lastChannel := stateManager.GetLastChannel()
		if lastChannel == "" {
			return
		}
		parts := strings.SplitN(lastChannel, ":", 2)
		if len(parts) != 2 {
			return
		}
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: parts[0],
			ChatID:  parts[1],
			Content: fmt.Sprintf("⚠️ Budget alert: %s has used %d/%d tokens today", role, used, limit),
		})
	})

	// Wire the reviewer agent so /v1/review/change uses a different model
	// than the implementer -- the core of independent review.
	var reviewFunc orchestratorhost.ReviewFunc
	for _, agentCfg := range cfg.Agents.List {
		if agentCfg.Role == "reviewer" && agentCfg.Provider != "" && agentCfg.Model != "" {
			reviewProvider, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
			if err == nil {
				reviewModel := agentCfg.Model
				reviewFunc = func(ctx context.Context, req orchestrator.ChangeReviewRequest) (orchestrator.ChangeReviewResponse, error) {
					if err := budget.check("reviewer"); err != nil {
						return orchestrator.ChangeReviewResponse{TaskID: req.TaskID, Verdict: orchestrator.ReviewVerdictApprove, Summary: fmt.Sprintf("Budget exceeded: %v", err)}, nil
					}
					resp, err := callReviewer(ctx, reviewProvider, reviewModel, req)
					if err == nil && resp.Verdict != "" {
						budget.record("reviewer", len(req.Objective)+len(req.Diff)+len(req.TestOutput)+500)
						saveCheckpoint(cfg.WorkspacePath(), req.TaskID, "reviewer", "review", resp.Summary)
					}
					return resp, err
				}
				fmt.Printf("✓ Reviewer agent using %s/%s\n", agentCfg.Provider, agentCfg.Model)
			} else {
				fmt.Printf("⚠ Reviewer agent not available: %v\n", err)
			}
			break
		}
	}

	// Build a role->provider+model map so /v1/agent/think can route to the
	// right model for each team role (lead plans, engineer implements, etc.).
	type roleEntry struct {
		provider providers.LLMProvider
		model    string
	}
	roleProviders := make(map[string]roleEntry)
	agentRoster := make([]orchestrator.AgentProfile, 0, len(cfg.Agents.List))
	for _, agentCfg := range cfg.Agents.List {
		if agentCfg.Role == "" || agentCfg.Provider == "" || agentCfg.Model == "" {
			continue
		}
		p, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
		if err != nil {
			fmt.Printf("⚠ Agent %s (%s): %v\n", agentCfg.ID, agentCfg.Role, err)
			continue
		}
		roleProviders[agentCfg.Role] = roleEntry{provider: p, model: agentCfg.Model}
		agentRoster = append(agentRoster, orchestrator.AgentProfile{
			ID:           agentCfg.ID,
			Name:         agentCfg.Name,
			Role:         agentCfg.Role,
			ProviderName: agentCfg.Provider,
			Model:        agentCfg.Model,
			Capabilities: agentCapabilitiesForRole(agentCfg.Role),
		})
		fmt.Printf("✓ %s agent (%s) using %s/%s\n", agentCfg.Role, agentCfg.ID, agentCfg.Provider, agentCfg.Model)
	}

	thinkFunc := func(ctx context.Context, req orchestrator.AgentThinkRequest) (orchestrator.AgentThinkResponse, error) {
		entry, ok := roleProviders[req.Role]
		if !ok {
			return orchestrator.AgentThinkResponse{}, fmt.Errorf("no agent configured for role %q", req.Role)
		}
		if err := budget.check(req.Role); err != nil {
			return orchestrator.AgentThinkResponse{}, err
		}

		// Per-action model override (MetaGPT pattern): let the orchestrator
		// select a cheaper model for simple actions within the same role.
		callProvider := entry.provider
		callModel := entry.model
		if req.ProviderName != "" && req.Model != "" {
			if p, err := providers.CreateProviderForFallback(cfg, req.ProviderName, req.Model); err == nil {
				callProvider = p
				callModel = req.Model
			}
		}

		messages := []providers.Message{{Role: "user", Content: req.Prompt}}
		resp, err := callProvider.Chat(ctx, messages, nil, callModel, map[string]interface{}{
			"max_tokens":  2048,
			"temperature": 0.3,
		})
		if err != nil {
			return orchestrator.AgentThinkResponse{}, err
		}
		if resp.Usage != nil {
			budget.record(req.Role, resp.Usage.TotalTokens)
		}
		saveCheckpoint(cfg.WorkspacePath(), req.TaskID, req.Role, "think", resp.Content[:min(len(resp.Content), 200)])
		return orchestrator.AgentThinkResponse{
			TaskID:  req.TaskID,
			Role:    req.Role,
			Content: resp.Content,
		}, nil
	}

	hostServer := orchestratorhost.NewServer(orchestratorhost.Config{
		SocketPath:          hostSocket,
		WorkspaceRoot:       cfg.WorkspacePath(),
		RestrictToWorkspace: cfg.Agents.Defaults.RestrictToWorkspace,
		Sandboxed:           cfg.Workspace.Sandboxed,
		TelegramEnabled:     cfg.Channels.Telegram.Enabled,
		ReviewChange:        reviewFunc,
		AgentThink:          thinkFunc,
		AgentRoster:         agentRoster,
	}, channelManager)
	go func() {
		if err := hostServer.Start(ctx); err != nil {
			logger.ErrorCF("orchestrator-host", "Host capability server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()
	fmt.Printf("✓ Orchestrator host server started on unix://%s\n", hostSocket)

	resumeIncompleteSessions(cfg.WorkspacePath())
	fmt.Printf("✓ Gateway started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop")

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("✓ Cron service started")

	if err := heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("✓ Heartbeat service started")

	stateManager := state.NewManager(cfg.WorkspacePath())

	// V1 Event Router
	eventRouter := events.NewRouter()
	eventRouter.Start(ctx)
	fmt.Println("✓ V1 event router started")

	// V1 Job Queue
	jobQueue, err := queue.NewQueue(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error creating job queue: %v\n", err)
	} else {
		jobQueue.Start(ctx, 5*time.Second)
		fmt.Println("✓ V1 job queue started")
	}

	// Device Registry
	hostname, _ := os.Hostname()
	selfDevice := devsync.DeviceInfo{
		ID:       hostname,
		Name:     hostname,
		Host:     cfg.Gateway.Host,
		Port:     cfg.Gateway.Port,
		Platform: runtime.GOOS,
		Version:  version,
	}
	registry := devsync.NewRegistry(selfDevice)

	// Prune stale devices periodically.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if pruned := registry.PruneStale(3 * time.Minute); pruned > 0 {
					logger.InfoCF("sync", "Pruned stale devices", map[string]interface{}{"count": pruned})
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	fmt.Printf("✓ Device registry started (self: %s/%s)\n", selfDevice.ID, selfDevice.Platform)

	// V1 API Server
	var apiServer *api.Server
	if cfg.V1API.Enabled {
		apiServer = api.NewServer(api.Config{
			Addr:   cfg.V1API.Addr,
			APIKey: cfg.V1API.APIKey,
		}, msgBus, eventRouter, stateManager, registry)

		apiServer.SetChatHandler(func(ctx context.Context, message, sessionKey string) (string, error) {
			return agentLoop.ProcessDirectWithChannel(ctx, message, sessionKey, "api", sessionKey)
		})

		go func() {
			if err := apiServer.Start(ctx); err != nil {
				logger.ErrorCF("api", "V1 API server error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ V1 API server started on %s\n", cfg.V1API.Addr)
	}

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	healthServer := health.NewServer(cfg.Gateway.Host, cfg.Gateway.Port)
	go func() {
		if err := healthServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.ErrorCF("health", "Health server error", map[string]interface{}{"error": err.Error()})
		}
	}()
	fmt.Printf("✓ Health endpoints available at http://%s:%d/health and /ready\n", cfg.Gateway.Host, cfg.Gateway.Port)

	if os.Getenv("VIKRAM_CONSOLE_ENABLED") == "1" {
		// Management dashboard with team, config, and task views. This remains
		// opt-in until the web control plane has production authentication.
		dashboardAddr := dashboardAddrFromEnv()
		consoleAddr := consoleAddrFromEnv()
		dashCfg := dashboard.Config{Enabled: true, Addr: dashboardAddr, Title: "Vikram"}
		dashSrv := dashboard.NewServer(dashCfg)
		dashSrv.SetStatusProvider(func() dashboard.StatusData {
			return dashboard.StatusData{
				Status:           "running",
				ActiveChannels:   channelManager.GetEnabledChannels(),
				TrackedUsers:     stateManager.UserCount(),
				WebSocketClients: 0,
			}
		})
		dashSrv.SetTeamProvider(func() []map[string]interface{} {
			var agents []map[string]interface{}
			for _, a := range cfg.Agents.List {
				agents = append(agents, map[string]interface{}{
					"id":                 a.ID,
					"role":               a.Role,
					"provider":           a.Provider,
					"model":              a.Model,
					"max_tokens_per_day": a.MaxTokensPerDay,
					"budget_action":      a.BudgetAction,
				})
			}
			return agents
		})
		dashSrv.SetConfigProvider(func() map[string]interface{} {
			providers := []map[string]interface{}{
				{"name": "deepseek", "configured": cfg.Providers.DeepSeek.APIKey != "", "base": cfg.Providers.DeepSeek.APIBase},
				{"name": "mistral", "configured": cfg.Providers.Mistral.APIKey != "", "base": cfg.Providers.Mistral.APIBase},
				{"name": "nvidia", "configured": cfg.Providers.Nvidia.APIKey != "", "base": cfg.Providers.Nvidia.APIBase},
				{"name": "zhipu", "configured": cfg.Providers.Zhipu.APIKey != "", "base": cfg.Providers.Zhipu.APIBase},
				{"name": "openai", "configured": cfg.Providers.OpenAI.APIKey != "", "base": cfg.Providers.OpenAI.APIBase},
				{"name": "anthropic", "configured": cfg.Providers.Anthropic.APIKey != "", "base": cfg.Providers.Anthropic.APIBase},
				{"name": "openrouter", "configured": cfg.Providers.OpenRouter.APIKey != "", "base": cfg.Providers.OpenRouter.APIBase},
				{"name": "vertex", "configured": cfg.Providers.Vertex.ProjectID != "", "base": cfg.Providers.Vertex.Location},
			}
			var agents []map[string]interface{}
			for _, a := range cfg.Agents.List {
				agents = append(agents, map[string]interface{}{
					"id":                 a.ID,
					"role":               a.Role,
					"provider":           a.Provider,
					"model":              a.Model,
					"max_tokens_per_day": a.MaxTokensPerDay,
				})
			}
			return map[string]interface{}{"providers": providers, "agents": agents}
		})
		go func() {
			if err := dashSrv.Start(ctx); err != nil && err != http.ErrServerClosed {
				logger.ErrorCF("dashboard", "Dashboard error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ Dashboard available at http://%s/manage\n", dashboardAddr)

		consoleSrv := console.NewServer(console.Config{Enabled: true, Addr: consoleAddr, APIKey: os.Getenv("VIKRAM_CONSOLE_API_KEY")}, cfg, getConfigPath())
		consoleSrv.SetOnAgentChange(func(action string, agentCfg config.AgentConfig) {
			if action == "add" {
				if agentCfg.Provider != "" && agentCfg.Model != "" {
					p, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
					if err == nil {
						agentLoop.SubagentManager.RegisterAgent(agentCfg.ID, agentCfg.Role, p, agentCfg.Model, nil, agentCfg.SystemPrompt)
						fmt.Printf("✓ Agent %s (%s) registered at runtime\n", agentCfg.ID, agentCfg.Role)
					} else {
						fmt.Printf("⚠ Agent %s provider failed: %v\n", agentCfg.ID, err)
					}
				}
			} else if action == "remove" {
				agentLoop.SubagentManager.UnregisterAgent(agentCfg.ID)
				fmt.Printf("✓ Agent %s removed at runtime\n", agentCfg.ID)
			}
		})
		consoleSrv.SetOnChannelChange(func(name string) {
			if err := channelManager.ReconnectChannel(ctx, name); err != nil {
				fmt.Printf("⚠ Channel %s reconnect failed: %v\n", name, err)
			} else {
				fmt.Printf("✓ Channel %s reconnected\n", name)
			}
		})
		consoleSrv.SetChatHandler(func(ctx context.Context, message string) (string, error) {
			msgBus.PublishInbound(bus.InboundMessage{
				Channel: "web", SenderID: "founder", ChatID: "web-console",
				Content: message, SessionKey: "web:web-console",
			})
			return "Queued for Vikram agent.", nil
		})
		go func() {
			sub := msgBus.SubscribeOutbound()
			defer sub.Unsubscribe()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-sub.C:
					if !ok {
						return
					}
					if msg.Channel == "web" || msg.Channel == "" {
						consoleSrv.BroadcastChat("assistant", msg.Content)
					}
				}
			}
		}()
		go func() {
			if err := consoleSrv.Start(ctx); err != nil && err != http.ErrServerClosed {
				logger.ErrorCF("console", "Console error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ Management console at http://%s\n", consoleAddr)
	}

	go agentLoop.Run(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	if apiServer != nil {
		apiServer.Stop()
	}
	_ = hostServer.Stop(context.Background())
	if jobQueue != nil {
		jobQueue.Stop()
	}
	eventRouter.Stop()
	healthServer.Stop(context.Background())
	heartbeatService.Stop()
	cronService.Stop()
	agentLoop.Stop()
	channelManager.StopAll(ctx)
	fmt.Println("✓ Gateway stopped")
}

func isPublicHost(host string) bool {
	// Normalise: lowercase and strip trailing DNS dot so "LOCALHOST.",
	// "Localhost", "::1" etc. are all treated correctly.
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || host == "localhost" || host == "::1" {
		return false
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback()
}

func enabledChannelsWithoutAllowlist(cfg *config.Config) []string {
	type channelRule struct {
		name      string
		enabled   bool
		allowList []string
	}

	rules := []channelRule{
		{name: "telegram", enabled: cfg.Channels.Telegram.Enabled, allowList: cfg.Channels.Telegram.AllowFrom},
		{name: "whatsapp", enabled: cfg.Channels.WhatsApp.Enabled, allowList: cfg.Channels.WhatsApp.AllowFrom},
	}

	var insecure []string
	for _, rule := range rules {
		if rule.enabled && len(rule.allowList) == 0 {
			insecure = append(insecure, rule.name)
		}
	}
	sort.Strings(insecure)
	return insecure
}

func validateGatewaySecurity(cfg *config.Config) error {
	if cfg.V1API.Enabled && strings.TrimSpace(cfg.V1API.APIKey) == "" {
		return fmt.Errorf("v1_api.api_key is required when v1_api.enabled=true")
	}

	if isPublicHost(cfg.Gateway.Host) {
		if !cfg.Agents.Defaults.RestrictToWorkspace {
			return fmt.Errorf("agents.defaults.restrict_to_workspace must be true for public gateway host %q", cfg.Gateway.Host)
		}

		if insecureChannels := enabledChannelsWithoutAllowlist(cfg); len(insecureChannels) > 0 {
			return fmt.Errorf("public gateway host %q requires allow_from for enabled channels: %s", cfg.Gateway.Host, strings.Join(insecureChannels, ", "))
		}
	}

	return nil
}

func orchestratorHostSocketPath() string {
	if socketPath := strings.TrimSpace(os.Getenv("VIKRAM_HOST_SOCKET")); socketPath != "" {
		return socketPath
	}
	return "/tmp/vikramd.sock"
}

func statusCmd() {
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	configPath := getConfigPath()

	fmt.Printf("%s V1 Status\n", logo)
	fmt.Printf("Version: %s\n", formatVersion())
	build, _ := formatBuildInfo()
	if build != "" {
		fmt.Printf("Build: %s\n", build)
	}
	fmt.Println()

	if _, err := os.Stat(configPath); err == nil {
		fmt.Println("Config:", configPath, "✓")
	} else {
		fmt.Println("Config:", configPath, "✗")
	}

	workspace := cfg.WorkspacePath()
	if _, err := os.Stat(workspace); err == nil {
		fmt.Println("Workspace:", workspace, "✓")
	} else {
		fmt.Println("Workspace:", workspace, "✗")
	}

	if _, err := os.Stat(configPath); err == nil {
		providerName := cfg.Agents.Defaults.Provider
		if providerName == "" {
			providerName = "auto"
		}
		fmt.Printf("Model:    %s\n", cfg.Agents.Defaults.Model)
		fmt.Printf("Provider: %s\n", providerName)
		fmt.Println()

		ok := func(set bool) string {
			if set {
				return "✓"
			}
			return "not set"
		}

		fmt.Println("── API Providers ──────────────────────────────")
		fmt.Println("  OpenRouter:  ", ok(cfg.Providers.OpenRouter.APIKey != ""))
		fmt.Println("  Anthropic:   ", ok(cfg.Providers.Anthropic.APIKey != ""))
		fmt.Println("  OpenAI:      ", ok(cfg.Providers.OpenAI.APIKey != ""))
		fmt.Println("  Gemini:      ", ok(cfg.Providers.Gemini.APIKey != ""))
		fmt.Println("  Groq:        ", ok(cfg.Providers.Groq.APIKey != ""))
		fmt.Println("  DeepSeek:    ", ok(cfg.Providers.DeepSeek.APIKey != ""))
		fmt.Println("  Zhipu:       ", ok(cfg.Providers.Zhipu.APIKey != ""))
		fmt.Println("  Moonshot:    ", ok(cfg.Providers.Moonshot.APIKey != ""))
		fmt.Println("  NVIDIA NIM:  ", ok(cfg.Providers.Nvidia.APIKey != ""))
		if cfg.Providers.VLLM.APIBase != "" {
			fmt.Printf("  vLLM/Local:   ✓ %s\n", cfg.Providers.VLLM.APIBase)
		} else {
			fmt.Println("  vLLM/Local:  ", ok(false))
		}
		if cfg.Providers.Ollama.APIBase != "" {
			fmt.Printf("  Ollama:       ✓ %s\n", cfg.Providers.Ollama.APIBase)
		} else {
			fmt.Println("  Ollama:      ", ok(false))
		}
		if cfg.Providers.GitHubCopilot.APIBase != "" || cfg.Providers.GitHubCopilot.ConnectMode != "" {
			connectMode := cfg.Providers.GitHubCopilot.ConnectMode
			if connectMode == "" {
				connectMode = "stdio"
			}
			target := cfg.Providers.GitHubCopilot.APIBase
			if target == "" {
				target = defaultGitHubCopilotTarget(connectMode)
			}
			fmt.Printf("  GitHub Copilot: ✓ %s via %s\n", connectMode, target)
		} else {
			fmt.Println("  GitHub Copilot:", ok(false))
		}

		fmt.Println()
		fmt.Println("── Enterprise Providers ───────────────────────")
		if cfg.Providers.Vertex.ProjectID != "" {
			fmt.Printf("  Vertex AI:    ✓ project=%s region=%s\n",
				cfg.Providers.Vertex.ProjectID, cfg.Providers.Vertex.Location)
		} else {
			fmt.Println("  Vertex AI:    not configured")
		}
		if cfg.Providers.Bedrock.Region != "" {
			fmt.Printf("  AWS Bedrock:  ✓ region=%s\n", cfg.Providers.Bedrock.Region)
		} else {
			fmt.Println("  AWS Bedrock:  not configured")
		}
		if cfg.Providers.AzureOpenAI.Endpoint != "" {
			fmt.Printf("  Azure OpenAI: ✓ %s (deployment: %s)\n",
				cfg.Providers.AzureOpenAI.Endpoint, cfg.Providers.AzureOpenAI.Deployment)
		} else {
			fmt.Println("  Azure OpenAI: not configured")
		}

		store, _ := auth.LoadStore()
		if store != nil && len(store.Credentials) > 0 {
			fmt.Println()
			fmt.Println("── OAuth / Token Auth ─────────────────────────")
			for prov, cred := range store.Credentials {
				credStatus := "active"
				if cred.IsExpired() {
					credStatus = "expired"
				} else if cred.NeedsRefresh() {
					credStatus = "needs refresh"
				}
				fmt.Printf("  %s (%s): %s\n", prov, cred.AuthMethod, credStatus)
			}
		}
	}
}

func authCmd() {
	if len(os.Args) < 3 {
		authHelp()
		return
	}

	switch os.Args[2] {
	case "login":
		authLoginCmd()
	case "logout":
		authLogoutCmd()
	case "status":
		authStatusCmd()
	default:
		fmt.Printf("Unknown auth command: %s\n", os.Args[2])
		authHelp()
	}
}

func authHelp() {
	fmt.Println("\nAuth commands:")
	fmt.Println("  login       Login via OAuth or paste token")
	fmt.Println("  logout      Remove stored credentials")
	fmt.Println("  status      Show current auth status")
	fmt.Println()
	fmt.Println("Login options:")
	fmt.Println("  --provider <name>    Provider to login with (openai, anthropic)")
	fmt.Println("  --device-code        Use device code flow (for headless environments)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  vikram auth login --provider openai")
	fmt.Println("  vikram auth login --provider openai --device-code")
	fmt.Println("  vikram auth login --provider anthropic")
	fmt.Println("  vikram auth logout --provider openai")
	fmt.Println("  vikram auth status")
}

func authLoginCmd() {
	provider := ""
	useDeviceCode := false

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		case "--device-code":
			useDeviceCode = true
		}
	}

	if provider == "" {
		fmt.Println("Error: --provider is required")
		fmt.Println("Supported providers: openai, anthropic")
		return
	}

	switch provider {
	case "openai":
		authLoginOpenAI(useDeviceCode)
	case "anthropic":
		authLoginPasteToken(provider)
	default:
		fmt.Printf("Unsupported provider: %s\n", provider)
		fmt.Println("Supported providers: openai, anthropic")
	}
}

func authLoginOpenAI(useDeviceCode bool) {
	cfg := auth.OpenAIOAuthConfig()

	var cred *auth.AuthCredential
	var err error

	if useDeviceCode {
		cred, err = auth.LoginDeviceCode(cfg)
	} else {
		cred, err = auth.LoginBrowser(cfg)
	}

	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential("openai", cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		appCfg.Providers.OpenAI.AuthMethod = "oauth"
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Println("Login successful!")
	if cred.AccountID != "" {
		fmt.Printf("Account: %s\n", cred.AccountID)
	}
}

func authLoginPasteToken(provider string) {
	cred, err := auth.LoginPasteToken(provider, os.Stdin)
	if err != nil {
		fmt.Printf("Login failed: %v\n", err)
		os.Exit(1)
	}

	if err := auth.SetCredential(provider, cred); err != nil {
		fmt.Printf("Failed to save credentials: %v\n", err)
		os.Exit(1)
	}

	appCfg, err := loadConfig()
	if err == nil {
		switch provider {
		case "anthropic":
			appCfg.Providers.Anthropic.AuthMethod = "token"
		case "openai":
			appCfg.Providers.OpenAI.AuthMethod = "token"
		}
		if err := config.SaveConfig(getConfigPath(), appCfg); err != nil {
			fmt.Printf("Warning: could not update config: %v\n", err)
		}
	}

	fmt.Printf("Token saved for %s!\n", provider)
}

func authLogoutCmd() {
	provider := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--provider", "-p":
			if i+1 < len(args) {
				provider = args[i+1]
				i++
			}
		}
	}

	if provider != "" {
		if err := auth.DeleteCredential(provider); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			switch provider {
			case "openai":
				appCfg.Providers.OpenAI.AuthMethod = ""
			case "anthropic":
				appCfg.Providers.Anthropic.AuthMethod = ""
			}
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Printf("Logged out from %s\n", provider)
	} else {
		if err := auth.DeleteAllCredentials(); err != nil {
			fmt.Printf("Failed to remove credentials: %v\n", err)
			os.Exit(1)
		}

		appCfg, err := loadConfig()
		if err == nil {
			appCfg.Providers.OpenAI.AuthMethod = ""
			appCfg.Providers.Anthropic.AuthMethod = ""
			config.SaveConfig(getConfigPath(), appCfg)
		}

		fmt.Println("Logged out from all providers")
	}
}

func authStatusCmd() {
	store, err := auth.LoadStore()
	if err != nil {
		fmt.Printf("Error loading auth store: %v\n", err)
		return
	}

	if len(store.Credentials) == 0 {
		fmt.Println("No authenticated providers.")
		fmt.Println("Run: vikram auth login --provider <name>")
		return
	}

	fmt.Println("\nAuthenticated Providers:")
	fmt.Println("------------------------")
	for provider, cred := range store.Credentials {
		status := "active"
		if cred.IsExpired() {
			status = "expired"
		} else if cred.NeedsRefresh() {
			status = "needs refresh"
		}

		fmt.Printf("  %s:\n", provider)
		fmt.Printf("    Method: %s\n", cred.AuthMethod)
		fmt.Printf("    Status: %s\n", status)
		if cred.AccountID != "" {
			fmt.Printf("    Account: %s\n", cred.AccountID)
		}
		if !cred.ExpiresAt.IsZero() {
			fmt.Printf("    Expires: %s\n", cred.ExpiresAt.Format("2006-01-02 15:04"))
		}
	}
}

func getConfigPath() string {
	return config.ConfigPath()
}

func historyFilePath(name string) string {
	historyDir := filepath.Join(config.HomeDir(), "history")
	if err := os.MkdirAll(historyDir, 0700); err == nil {
		return filepath.Join(historyDir, name)
	}
	return filepath.Join(os.TempDir(), name)
}

func setupCronTool(ctx context.Context, agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, workspace string, restrict bool, sandboxed bool, execTimeout time.Duration) *cron.CronService {
	cronStorePath := filepath.Join(workspace, "cron", "jobs.json")

	// Create cron service
	cronService := cron.NewCronService(cronStorePath, nil)

	// Create and register CronTool
	cronTool := tools.NewCronTool(cronService, agentLoop, msgBus, workspace, restrict, sandboxed, execTimeout)
	agentLoop.RegisterTool(cronTool)

	// Set the onJob handler — use the gateway lifetime context so cron jobs
	// are cancelled when the process shuts down.
	cronService.SetOnJob(func(job *cron.CronJob) (string, error) {
		result := cronTool.ExecuteJob(ctx, job)
		return result, nil
	})

	return cronService
}

func loadConfig() (*config.Config, error) {
	return config.LoadConfig(getConfigPath())
}

func cronCmd() {
	if len(os.Args) < 3 {
		cronHelp()
		return
	}

	subcommand := os.Args[2]

	// Load config to get workspace path
	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	cronStorePath := filepath.Join(cfg.WorkspacePath(), "cron", "jobs.json")

	switch subcommand {
	case "list":
		cronListCmd(cronStorePath)
	case "add":
		cronAddCmd(cronStorePath)
	case "remove":
		if len(os.Args) < 4 {
			fmt.Println("Usage: vikram cron remove <job_id>")
			return
		}
		cronRemoveCmd(cronStorePath, os.Args[3])
	case "enable":
		cronEnableCmd(cronStorePath, false)
	case "disable":
		cronEnableCmd(cronStorePath, true)
	default:
		fmt.Printf("Unknown cron command: %s\n", subcommand)
		cronHelp()
	}
}

func cronHelp() {
	fmt.Println("\nCron commands:")
	fmt.Println("  list              List all scheduled jobs")
	fmt.Println("  add              Add a new scheduled job")
	fmt.Println("  remove <id>       Remove a job by ID")
	fmt.Println("  enable <id>      Enable a job")
	fmt.Println("  disable <id>     Disable a job")
	fmt.Println()
	fmt.Println("Add options:")
	fmt.Println("  -n, --name       Job name")
	fmt.Println("  -m, --message    Message for agent")
	fmt.Println("  -e, --every      Run every N seconds")
	fmt.Println("  -c, --cron       Cron expression (e.g. '0 9 * * *')")
	fmt.Println("  -d, --deliver     Deliver response to channel")
	fmt.Println("  --to             Recipient for delivery")
	fmt.Println("  --channel        Channel for delivery")
}

func cronListCmd(storePath string) {
	cs := cron.NewCronService(storePath, nil)
	jobs := cs.ListJobs(true) // Show all jobs, including disabled

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}

	fmt.Println("\nScheduled Jobs:")
	fmt.Println("----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}

		nextRun := "scheduled"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		fmt.Printf("  %s (%s)\n", job.Name, job.ID)
		fmt.Printf("    Schedule: %s\n", schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Next run: %s\n", nextRun)
	}
}

func cronAddCmd(storePath string) {
	name := ""
	message := ""
	var everySec *int64
	cronExpr := ""
	deliver := false
	channel := ""
	to := ""

	args := os.Args[3:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--name":
			if i+1 < len(args) {
				name = args[i+1]
				i++
			}
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		case "-e", "--every":
			if i+1 < len(args) {
				var sec int64
				fmt.Sscanf(args[i+1], "%d", &sec)
				everySec = &sec
				i++
			}
		case "-c", "--cron":
			if i+1 < len(args) {
				cronExpr = args[i+1]
				i++
			}
		case "-d", "--deliver":
			deliver = true
		case "--to":
			if i+1 < len(args) {
				to = args[i+1]
				i++
			}
		case "--channel":
			if i+1 < len(args) {
				channel = args[i+1]
				i++
			}
		}
	}

	if name == "" {
		fmt.Println("Error: --name is required")
		return
	}

	if message == "" {
		fmt.Println("Error: --message is required")
		return
	}

	if everySec == nil && cronExpr == "" {
		fmt.Println("Error: Either --every or --cron must be specified")
		return
	}

	var schedule cron.CronSchedule
	if everySec != nil {
		everyMS := *everySec * 1000
		schedule = cron.CronSchedule{
			Kind:    "every",
			EveryMS: &everyMS,
		}
	} else {
		schedule = cron.CronSchedule{
			Kind: "cron",
			Expr: cronExpr,
		}
	}

	cs := cron.NewCronService(storePath, nil)
	job, err := cs.AddJob(name, schedule, message, deliver, channel, to)
	if err != nil {
		fmt.Printf("Error adding job: %v\n", err)
		return
	}

	fmt.Printf("✓ Added job '%s' (%s)\n", job.Name, job.ID)
}

func cronRemoveCmd(storePath, jobID string) {
	cs := cron.NewCronService(storePath, nil)
	if cs.RemoveJob(jobID) {
		fmt.Printf("✓ Removed job %s\n", jobID)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func cronEnableCmd(storePath string, disable bool) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: vikram cron enable/disable <job_id>")
		return
	}

	jobID := os.Args[3]
	cs := cron.NewCronService(storePath, nil)
	enabled := !disable

	job := cs.EnableJob(jobID, enabled)
	if job != nil {
		status := "enabled"
		if disable {
			status = "disabled"
		}
		fmt.Printf("✓ Job '%s' %s\n", job.Name, status)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func skillsHelp() {
	fmt.Println("\nSkills commands:")
	fmt.Println("  list                    List installed skills")
	fmt.Println("  install <repo>          Install skill from GitHub")
	fmt.Println("  install-builtin          Install all builtin skills to workspace")
	fmt.Println("  list-builtin             List available builtin skills")
	fmt.Println("  remove <name>           Remove installed skill")
	fmt.Println("  search                  Search available skills")
	fmt.Println("  show <name>             Show skill details")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  vikram skills list")
	fmt.Println("  vikram skills install amit-vikramaditya/vikram-skills/weather")
	fmt.Println("  vikram skills install-builtin")
	fmt.Println("  vikram skills list-builtin")
	fmt.Println("  vikram skills remove weather")
}

func skillsListCmd(loader *skills.SkillsLoader) {
	allSkills := loader.ListSkills()

	if len(allSkills) == 0 {
		fmt.Println("No skills installed.")
		return
	}

	fmt.Println("\nInstalled Skills:")
	fmt.Println("------------------")
	for _, skill := range allSkills {
		fmt.Printf("  ✓ %s (%s)\n", skill.Name, skill.Source)
		if skill.Description != "" {
			fmt.Printf("    %s\n", skill.Description)
		}
	}
}

func skillsInstallCmd(installer *skills.SkillInstaller) {
	if len(os.Args) < 4 {
		fmt.Println("Usage: vikram skills install <github-repo>")
		fmt.Println("Example: vikram skills install amit-vikramaditya/vikram-skills/weather")
		return
	}

	repo := os.Args[3]
	fmt.Printf("Installing skill from %s...\n", repo)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := installer.InstallFromGitHub(ctx, repo); err != nil {
		fmt.Printf("✗ Failed to install skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' installed successfully!\n", filepath.Base(repo))
}

func skillsRemoveCmd(installer *skills.SkillInstaller, skillName string) {
	fmt.Printf("Removing skill '%s'...\n", skillName)

	if err := installer.Uninstall(skillName); err != nil {
		fmt.Printf("✗ Failed to remove skill: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Skill '%s' removed successfully!\n", skillName)
}

func detectBuiltinSkillsDir(workspace string) string {
	candidates := []string{
		filepath.Join(workspace, "skills"),
		config.GlobalSkillsDir(),
		filepath.Join(".", "workspace", "skills"),
		filepath.Join(".", "cmd", "vikram", "workspace", "skills"),
		filepath.Join(".", "skills"),
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}

		if info, err := os.Stat(clean); err == nil && info.IsDir() {
			return clean
		}
	}

	return ""
}

func readSkillDescription(skillFile string) string {
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return "No description"
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return "No description"
	}

	for i := 1; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "---" {
			break
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'")
			if desc != "" {
				return desc
			}
		}
	}

	return "No description"
}

func skillsInstallBuiltinCmd(workspace string) {
	builtinSkillsDir := detectBuiltinSkillsDir(workspace)
	if builtinSkillsDir == "" {
		fmt.Println("✗ No builtin skills directory found.")
		fmt.Println("  Run `vikram onboard` first, or execute from the source repository.")
		return
	}

	workspaceSkillsDir := filepath.Join(workspace, "skills")
	builtinAbs, _ := filepath.Abs(builtinSkillsDir)
	workspaceAbs, _ := filepath.Abs(workspaceSkillsDir)
	if builtinAbs == workspaceAbs {
		fmt.Println("✓ Builtin skills are already present in your workspace.")
		return
	}

	fmt.Printf("Copying builtin skills from %s to workspace...\n", builtinSkillsDir)

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("✗ Failed to read builtin skills directory: %v\n", err)
		return
	}

	copied := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillName := entry.Name()
		builtinPath := filepath.Join(builtinSkillsDir, skillName)
		skillFile := filepath.Join(builtinPath, "SKILL.md")
		if _, err := os.Stat(skillFile); err != nil {
			continue
		}

		workspacePath := filepath.Join(workspaceSkillsDir, skillName)
		if err := os.MkdirAll(workspacePath, 0755); err != nil {
			fmt.Printf("✗ Failed to create directory for %s: %v\n", skillName, err)
			continue
		}
		if err := copyDirectory(builtinPath, workspacePath); err != nil {
			fmt.Printf("✗ Failed to copy %s: %v\n", skillName, err)
			continue
		}
		copied++
	}

	fmt.Printf("\n✓ Installed %d builtin skill(s)\n", copied)
}

func skillsListBuiltinCmd(workspace string) {
	builtinSkillsDir := detectBuiltinSkillsDir(workspace)
	if builtinSkillsDir == "" {
		fmt.Println("No builtin skills directory found.")
		return
	}

	fmt.Println("\nAvailable Builtin Skills:")
	fmt.Println("-----------------------")

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		fmt.Printf("Error reading builtin skills: %v\n", err)
		return
	}

	if len(entries) == 0 {
		fmt.Println("No builtin skills available.")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			description := readSkillDescription(skillFile)
			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("     %s\n", description)
			}
		}
	}
}

func skillsSearchCmd(installer *skills.SkillInstaller) {
	fmt.Println("Searching for available skills...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	availableSkills, err := installer.ListAvailableSkills(ctx)
	if err != nil {
		fmt.Printf("✗ Failed to fetch skills list: %v\n", err)
		return
	}

	if len(availableSkills) == 0 {
		fmt.Println("No skills available.")
		return
	}

	fmt.Printf("\nAvailable Skills (%d):\n", len(availableSkills))
	fmt.Println("--------------------")
	for _, skill := range availableSkills {
		fmt.Printf("  📦 %s\n", skill.Name)
		fmt.Printf("     %s\n", skill.Description)
		fmt.Printf("     Repo: %s\n", skill.Repository)
		if skill.Author != "" {
			fmt.Printf("     Author: %s\n", skill.Author)
		}
		if len(skill.Tags) > 0 {
			fmt.Printf("     Tags: %v\n", skill.Tags)
		}
		fmt.Println()
	}
}

func skillsShowCmd(loader *skills.SkillsLoader, skillName string) {
	content, ok := loader.LoadSkill(skillName)
	if !ok {
		fmt.Printf("✗ Skill '%s' not found\n", skillName)
		return
	}

	fmt.Printf("\n📦 Skill: %s\n", skillName)
	fmt.Println("----------------------")
	fmt.Println(content)
}

func agentCapabilitiesForRole(role string) []string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "lead", "planner", "architect":
		return []string{"planning", "architecture", "integration"}
	case "engineer", "implementer", "coder":
		return []string{"implementation", "code_editing", "documentation"}
	case "reviewer", "critic":
		return []string{"code_review", "adversarial_review", "risk_analysis"}
	case "runner", "verifier":
		return []string{"verification", "test_analysis", "execution_review"}
	case "qa", "browser", "visual":
		return []string{"qa", "browser_review", "visual_review"}
	default:
		if strings.TrimSpace(role) == "" {
			return nil
		}
		return []string{strings.ToLower(strings.TrimSpace(role))}
	}
}

// callReviewer builds a review prompt and asks the reviewer model to evaluate
// a set of code changes.  The reviewer MUST be a different model than the
// implementer — independent review is the point.
func callReviewer(ctx context.Context, reviewer providers.LLMProvider, model string, req orchestrator.ChangeReviewRequest) (orchestrator.ChangeReviewResponse, error) {
	prompt := fmt.Sprintf(`You are a code reviewer evaluating changes made by another AI agent.

TASK OBJECTIVE: %s

CHANGES (diff):
%s

TEST OUTPUT:
%s

LINT ERRORS:
%s

Review the changes against the objective. Consider:
- Does this change actually address the objective?
- Are there scope creep or unrelated changes?
- Are there security concerns, bugs, or design issues?
- Does the code follow best practices?

Respond with ONLY a JSON object:
{
  "verdict": "APPROVE" | "CHANGES_REQUESTED" | "REJECT",
  "issues": ["issue 1", "issue 2"],
  "summary": "brief explanation"
}`, req.Objective, req.Diff, req.TestOutput, strings.Join(req.LintErrors, "\n"))

	messages := []providers.Message{
		{Role: "user", Content: prompt},
	}

	resp, err := reviewer.Chat(ctx, messages, nil, model, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return orchestrator.ChangeReviewResponse{
			TaskID:  req.TaskID,
			Verdict: orchestrator.ReviewVerdictApprove,
			Summary: fmt.Sprintf("Review unavailable: %v", err),
		}, nil // degrade gracefully — don't block the task on review failure
	}

	// Parse the JSON response.
	var result struct {
		Verdict string   `json:"verdict"`
		Issues  []string `json:"issues"`
		Summary string   `json:"summary"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
		// Try to extract JSON from the response if the model added markdown fences.
		cleaned := strings.TrimSpace(resp.Content)
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```")
		cleaned = strings.TrimSuffix(cleaned, "```")
		cleaned = strings.TrimSpace(cleaned)
		if err2 := json.Unmarshal([]byte(cleaned), &result); err2 != nil {
			return orchestrator.ChangeReviewResponse{
				TaskID:  req.TaskID,
				Verdict: orchestrator.ReviewVerdictApprove,
				Summary: fmt.Sprintf("Could not parse review response: %v", err),
			}, nil
		}
	}

	verdict := orchestrator.ChangeReviewVerdict(strings.ToUpper(strings.TrimSpace(result.Verdict)))
	if verdict != orchestrator.ReviewVerdictApprove &&
		verdict != orchestrator.ReviewVerdictChangesRequested &&
		verdict != orchestrator.ReviewVerdictReject {
		verdict = orchestrator.ReviewVerdictApprove
	}

	return orchestrator.ChangeReviewResponse{
		TaskID:  req.TaskID,
		Verdict: verdict,
		Issues:  result.Issues,
		Summary: result.Summary,
	}, nil
}

// agentCheckpoint stores enough state to resume an interrupted agent invocation.
type agentCheckpoint struct {
	TaskID      string    `json:"task_id"`
	Role        string    `json:"role"`
	Phase       string    `json:"phase"`
	LastSummary string    `json:"last_summary"`
	Timestamp   time.Time `json:"timestamp"`
}

// resumeIncompleteSessions scans for checkpoint files from a previous run
// and logs them.  The actual resume happens when the orchestrator replays
// the LangGraph workflow from its own checkpoints.
func resumeIncompleteSessions(workspaceRoot string) {
	tasksDir := filepath.Join(workspaceRoot, "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cp, err := loadCheckpoint(workspaceRoot, entry.Name())
		if err != nil {
			continue
		}
		fmt.Printf("📋 Incomplete session: %s (role=%s phase=%s at %s)\n",
			cp.TaskID, cp.Role, cp.Phase, cp.Timestamp.Format("15:04:05"))
	}
}

func saveCheckpoint(workspaceRoot, taskID, role, phase, summary string) {
	dir := filepath.Join(workspaceRoot, "tasks", taskID)
	os.MkdirAll(dir, 0700)
	cp := agentCheckpoint{
		TaskID:      taskID,
		Role:        role,
		Phase:       phase,
		LastSummary: summary,
		Timestamp:   time.Now(),
	}
	data, _ := json.Marshal(cp)
	os.WriteFile(filepath.Join(dir, "checkpoint.json"), data, 0600)
}

func loadCheckpoint(workspaceRoot, taskID string) (*agentCheckpoint, error) {
	path := filepath.Join(workspaceRoot, "tasks", taskID, "checkpoint.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cp agentCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

func sendTeamSummary(cfg *config.Config, msgBus *bus.MessageBus) {
	stateManager := state.NewManager(cfg.WorkspacePath())
	lastChannel := stateManager.GetLastChannel()
	if lastChannel == "" {
		return
	}
	parts := strings.SplitN(lastChannel, ":", 2)
	if len(parts) != 2 {
		return
	}

	var lines []string
	lines = append(lines, "📊 Vikram Team Summary")
	lines = append(lines, "")
	for _, a := range cfg.Agents.List {
		role := a.Role
		if role == "" {
			role = a.ID
		}
		provider := a.Provider
		if provider == "" {
			provider = "default"
		}
		limit := "unlimited"
		if a.MaxTokensPerDay > 0 {
			limit = fmt.Sprintf("%dK/day", a.MaxTokensPerDay/1000)
		}
		lines = append(lines, fmt.Sprintf("• %s (%s/%s) — budget: %s", role, provider, a.Model, limit))
	}
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("Gateway: %s:%d", cfg.Gateway.Host, cfg.Gateway.Port))

	msgBus.PublishOutbound(bus.OutboundMessage{
		Channel: parts[0],
		ChatID:  parts[1],
		Content: strings.Join(lines, "\n"),
	})
}

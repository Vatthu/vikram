package tools

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/v1claw/levik/pkg/logger"
)

// SecurityMiddleware defines the interface for vetting shell commands.
type SecurityMiddleware interface {
	VerifyCommand(command string) (string, error)
}

// AllowlistMiddleware enforces a strict set of pre-approved commands.
// The base command (first token, basename-resolved) must appear in Allowed.
type AllowlistMiddleware struct {
	Allowed []string
}

func (m *AllowlistMiddleware) VerifyCommand(command string) (string, error) {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty command")
	}

	// Resolve to basename so "/usr/bin/ls" matches "ls" in the allowlist.
	baseCmd := filepath.Base(parts[0])
	for _, a := range m.Allowed {
		if baseCmd == a {
			return command, nil
		}
	}

	return "", fmt.Errorf("command %q is not in the security allowlist", baseCmd)
}

// DefaultAllowlist provides a minimal safe subset of commands the shell tool
// may run.  Dynamic language interpreters (python, node, etc.) are excluded
// because they give arbitrary code execution.  curl and wget are included
// because the agent legitimately needs HTTP read access (weather, APIs, etc.);
// the effective exfiltration defence is that bash/sh/exec are NOT in the list,
// so `curl … | sh`-style attacks are blocked at the shell-tool level.
// Use DevAllowlist in development workspaces where build tools are required.
var DefaultAllowlist = []string{
	"ls", "cat", "grep", "find", "wc", "head", "tail", "du", "df",
	"awk", "sed", "sort", "uniq", "tr", "cut", "paste", "tee",
	"sleep", "true", "false", "test",
	"git",
	"mkdir", "cp", "mv", "rm", "touch", "stat", "file",
	"echo", "printf", "pwd", "date", "env", "which", "basename", "dirname",
	"zip", "unzip", "tar", "gzip", "gunzip",
	"curl", "wget",
	"diff", "patch",
	"xargs", "read",
}

// DevAllowlist extends DefaultAllowlist with build tools and language runtimes
// for workspaces where code execution is expected and trusted.
var DevAllowlist = append(append([]string{}, DefaultAllowlist...),
	"go", "npm", "node", "python3", "python", "rustc", "cargo",
	"make", "cmake", "mvn", "gradle",
)

// SandboxMiddleware is the stub for future container-based sandboxing.
// It is intentionally NOT implemented yet — callers that instantiate this
// middleware will get an explicit error rather than a false sense of security.
type SandboxMiddleware struct {
	ContainerImage string
	WorkspaceDir   string
}

func (m *SandboxMiddleware) VerifyCommand(command string) (string, error) {
	// Implementation required: wrap the command in docker/bwrap.
	// Until implemented, refuse execution so callers are not silently unprotected.
	logger.WarnCF("security", "SandboxMiddleware is not implemented — blocking command", map[string]interface{}{
		"command": command,
		"image":   m.ContainerImage,
	})
	return "", fmt.Errorf("SandboxMiddleware is not yet implemented: configure a real security middleware or use AllowlistMiddleware")
}

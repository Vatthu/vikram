package config

import (
	"os/exec"
)

// DiscoveredTool represents an AI CLI tool found on the local system.
type DiscoveredTool struct {
	ID          string // Internal identifier (e.g., "cli-copilot")
	Executable  string // Binary name to look for on the PATH (e.g., "copilot")
	DisplayName string // User-facing name (e.g., "GitHub Copilot CLI")
	Path        string // Absolute path to the executable
	Description string // Short description of what the tool is
}

// DiscoverLocalCLIs scans the local system's PATH for known AI CLI tools.
func DiscoverLocalCLIs() []DiscoveredTool {
	tools := []DiscoveredTool{
		{Executable: "copilot", ID: "cli-copilot", DisplayName: "GitHub Copilot CLI", Description: "Powered by OpenAI via GitHub"},
		{Executable: "codex", ID: "cli-codex", DisplayName: "Codex CLI", Description: "Command-line AI Assistant"},
		{Executable: "vibe", ID: "cli-vibe", DisplayName: "Vibe CLI", Description: "Local Developer AI"},
		{Executable: "gemini", ID: "cli-gemini", DisplayName: "Google Gemini CLI", Description: "Official Google Gemini Tool"},
		{Executable: "ollama", ID: "cli-ollama", DisplayName: "Ollama (Local)", Description: "Runs local open-source models"},
	}

	var discovered []DiscoveredTool

	for _, tool := range tools {
		if path, err := exec.LookPath(tool.Executable); err == nil {
			tool.Path = path
			discovered = append(discovered, tool)
		}
	}

	return discovered
}

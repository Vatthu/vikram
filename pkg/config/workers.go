package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CLIWorkerConfig defines how the user registers a new AI CLI in their config.
type CLIWorkerConfig struct {
	Name             string   `json:"name"`
	Command          string   `json:"command"`
	PromptFlag       string   `json:"prompt_flag"`        // e.g. "--prompt", "-m", "exec"
	AutoApproveFlags []string `json:"auto_approve_flags"` // e.g. ["--yolo", "-y", "--yes-always"]
	OutputFormatFlag string   `json:"output_format_flag"` // e.g. "--output json", "--silent"
	Enabled          bool     `json:"enabled"`
}

// LoadCLIWorkers loads external CLI worker definitions from a config file.
func LoadCLIWorkers(workspace string) ([]CLIWorkerConfig, error) {
	configPath := filepath.Join(workspace, "workers.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []CLIWorkerConfig{}, nil
		}
		return nil, fmt.Errorf("failed to read workers.json: %w", err)
	}

	var workers []CLIWorkerConfig
	if err := json.Unmarshal(data, &workers); err != nil {
		return nil, fmt.Errorf("failed to parse workers.json: %w", err)
	}

	return workers, nil
}

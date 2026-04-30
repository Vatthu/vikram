package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestDefaultConfig_HeartbeatEnabled verifies heartbeat is enabled by default
func TestDefaultConfig_HeartbeatEnabled(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be enabled by default")
	}
}

// TestDefaultConfig_WorkspacePath verifies workspace path is correctly set
func TestDefaultConfig_WorkspacePath(t *testing.T) {
	cfg := DefaultConfig()

	// Just verify the workspace is set, don't compare exact paths
	// since expandHome behavior may differ based on environment
	if cfg.Agents.Defaults.Workspace == "" {
		t.Error("Workspace should not be empty")
	}
}

// TestDefaultConfig_Model verifies model default is empty (set during interactive onboard)
func TestDefaultConfig_Model(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.Model != "" {
		t.Errorf("Model should be empty by default, got %q", cfg.Agents.Defaults.Model)
	}
}

// TestDefaultConfig_MaxTokens verifies max tokens has default value
func TestDefaultConfig_MaxTokens(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.MaxTokens == 0 {
		t.Error("MaxTokens should not be zero")
	}
}

// TestDefaultConfig_MaxToolIterations verifies max tool iterations has default value
func TestDefaultConfig_MaxToolIterations(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.MaxToolIterations == 0 {
		t.Error("MaxToolIterations should not be zero")
	}
}

// TestDefaultConfig_Temperature verifies temperature has default value
func TestDefaultConfig_Temperature(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.Temperature == 0 {
		t.Error("Temperature should not be zero")
	}
}

// TestDefaultConfig_Gateway verifies gateway defaults
func TestDefaultConfig_Gateway(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Gateway.Host != "127.0.0.1" {
		t.Error("Gateway host should have default value")
	}
	if cfg.Gateway.Port == 0 {
		t.Error("Gateway port should have default value")
	}
}

func TestHomeDir_UsesEnvOverride(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir failed: %v", err)
	}

	t.Setenv(HomeEnvVar, "~/custom-levik-home")
	want := filepath.Join(home, "custom-levik-home")
	if got := HomeDir(); got != want {
		t.Fatalf("HomeDir() = %q, want %q", got, want)
	}
}

func TestConfigPath_UsesHomeDir(t *testing.T) {
	t.Setenv(HomeEnvVar, "/tmp/levik-home")
	want := filepath.Join("/tmp/levik-home", "config.json")
	if got := ConfigPath(); got != want {
		t.Fatalf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveHomeDir_WindowsPrefersUserConfigDir(t *testing.T) {
	got := resolveHomeDir("windows", `C:\Users\Amit\.levik`, false, `C:\Users\Amit\AppData\Roaming`)
	want := filepath.Join(`C:\Users\Amit\AppData\Roaming`, "LeVik")
	if got != want {
		t.Fatalf("resolveHomeDir() = %q, want %q", got, want)
	}
}

func TestResolveHomeDir_WindowsKeepsLegacyHomeIfPresent(t *testing.T) {
	legacy := `C:\Users\Amit\.levik`
	got := resolveHomeDir("windows", legacy, true, `C:\Users\Amit\AppData\Roaming`)
	if got != legacy {
		t.Fatalf("resolveHomeDir() = %q, want %q", got, legacy)
	}
}

// TestDefaultConfig_Providers verifies provider structure
func TestDefaultConfig_Providers(t *testing.T) {
	cfg := DefaultConfig()

	// Verify all providers are empty by default
	if cfg.Providers.Anthropic.APIKey != "" {
		t.Error("Anthropic API key should be empty by default")
	}
	if cfg.Providers.OpenAI.APIKey != "" {
		t.Error("OpenAI API key should be empty by default")
	}
	if cfg.Providers.OpenRouter.APIKey != "" {
		t.Error("OpenRouter API key should be empty by default")
	}
	if cfg.Providers.Groq.APIKey != "" {
		t.Error("Groq API key should be empty by default")
	}
	if cfg.Providers.Zhipu.APIKey != "" {
		t.Error("Zhipu API key should be empty by default")
	}
	if cfg.Providers.VLLM.APIKey != "" {
		t.Error("VLLM API key should be empty by default")
	}
	if cfg.Providers.Gemini.APIKey != "" {
		t.Error("Gemini API key should be empty by default")
	}
}

// TestDefaultConfig_Channels verifies channels are disabled by default
func TestDefaultConfig_Channels(t *testing.T) {
	cfg := DefaultConfig()

	// Verify all channels are disabled by default
	if cfg.Channels.WhatsApp.Enabled {
		t.Error("WhatsApp should be disabled by default")
	}
	if cfg.Channels.Telegram.Enabled {
		t.Error("Telegram should be disabled by default")
	}
}

// TestDefaultConfig_WebTools verifies web tools config
func TestDefaultConfig_WebTools(t *testing.T) {
	cfg := DefaultConfig()

	// Verify web tools defaults
	if cfg.Tools.Web.Brave.MaxResults != 5 {
		t.Error("Expected Brave MaxResults 5, got ", cfg.Tools.Web.Brave.MaxResults)
	}
	if cfg.Tools.Web.Brave.APIKey != "" {
		t.Error("Brave API key should be empty by default")
	}
	if cfg.Tools.Web.DuckDuckGo.MaxResults != 5 {
		t.Error("Expected DuckDuckGo MaxResults 5, got ", cfg.Tools.Web.DuckDuckGo.MaxResults)
	}
}

func TestSaveConfig_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("config file has permission %04o, want 0600", perm)
	}
}

func TestSaveConfig_NormalizesWorkspaceFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.Workspace.Path = filepath.Join(tmpDir, "custom-workspace")
	cfg.Agents.Defaults.Workspace = DefaultWorkspaceDir()

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	if cfg.Agents.Defaults.Workspace != cfg.Workspace.Path {
		t.Fatalf("workspace fields not normalized in memory: %q vs %q", cfg.Workspace.Path, cfg.Agents.Defaults.Workspace)
	}

	var saved struct {
		Workspace struct {
			Path string `json:"path"`
		} `json:"workspace"`
		Agents struct {
			Defaults struct {
				Workspace string `json:"workspace"`
			} `json:"defaults"`
		} `json:"agents"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if saved.Workspace.Path != cfg.Workspace.Path {
		t.Fatalf("saved workspace.path = %q, want %q", saved.Workspace.Path, cfg.Workspace.Path)
	}
	if saved.Agents.Defaults.Workspace != cfg.Workspace.Path {
		t.Fatalf("saved agents.defaults.workspace = %q, want %q", saved.Agents.Defaults.Workspace, cfg.Workspace.Path)
	}
}

func TestLoadConfig_NormalizesWorkspaceFields(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	customWorkspace := filepath.Join(tmpDir, "custom-workspace")

	data := []byte(`{
  "workspace": {
    "path": ` + `"` + customWorkspace + `"` + `
  },
  "agents": {
    "defaults": {
      "workspace": ""
    }
  }
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Workspace.Path != customWorkspace {
		t.Fatalf("workspace.path = %q, want %q", cfg.Workspace.Path, customWorkspace)
	}
	if cfg.Agents.Defaults.Workspace != customWorkspace {
		t.Fatalf("agents.defaults.workspace = %q, want %q", cfg.Agents.Defaults.Workspace, customWorkspace)
	}
	if cfg.WorkspacePath() != customWorkspace {
		t.Fatalf("WorkspacePath() = %q, want %q", cfg.WorkspacePath(), customWorkspace)
	}
}

func TestLoadConfig_PreservesLegacyAgentWorkspaceWhenWorkspacePathIsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	legacyWorkspace := filepath.Join(tmpDir, "legacy-workspace")
	defaultWorkspace := DefaultWorkspaceDir()

	data := []byte(`{
  "workspace": {
    "path": ` + `"` + defaultWorkspace + `"` + `
  },
  "agents": {
    "defaults": {
      "workspace": ` + `"` + legacyWorkspace + `"` + `
    }
  }
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Workspace.Path != legacyWorkspace {
		t.Fatalf("workspace.path = %q, want %q", cfg.Workspace.Path, legacyWorkspace)
	}
	if cfg.Agents.Defaults.Workspace != legacyWorkspace {
		t.Fatalf("agents.defaults.workspace = %q, want %q", cfg.Agents.Defaults.Workspace, legacyWorkspace)
	}
}

func TestLoadConfig_PermissionsIncludeNotificationsAndScreen(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	data := []byte(`{
  "permissions": {
    "notifications": true,
    "screen": true
  }
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if !cfg.Permissions.Notifications {
		t.Fatal("permissions.notifications should be true")
	}
	if !cfg.Permissions.Screen {
		t.Fatal("permissions.screen should be true")
	}
}

func TestLoadConfig_ExpandsChannelEnvVars(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	t.Setenv("LEVIK_TELEGRAM_TEST_TOKEN", "telegram-secret")

	data := []byte(`{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "${LEVIK_TELEGRAM_TEST_TOKEN}"
    }
  }
}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Channels.Telegram.Token != "telegram-secret" {
		t.Fatalf("channels.telegram.token = %q, want %q", cfg.Channels.Telegram.Token, "telegram-secret")
	}
}

// TestConfig_Complete verifies all config fields are set
func TestConfig_Complete(t *testing.T) {
	cfg := DefaultConfig()

	// Verify complete config structure
	if cfg.Agents.Defaults.Workspace == "" {
		t.Error("Workspace should not be empty")
	}
	// Model is empty by default (set during interactive onboard)
	if cfg.Agents.Defaults.Temperature == 0 {
		t.Error("Temperature should have default value")
	}
	if cfg.Agents.Defaults.MaxTokens == 0 {
		t.Error("MaxTokens should not be zero")
	}
	if cfg.Agents.Defaults.MaxToolIterations == 0 {
		t.Error("MaxToolIterations should not be zero")
	}
	if cfg.Gateway.Host != "127.0.0.1" {
		t.Error("Gateway host should have default value")
	}
	if cfg.Gateway.Port == 0 {
		t.Error("Gateway port should have default value")
	}
	if !cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be enabled by default")
	}
}

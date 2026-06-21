package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/Vatthu/vikram/pkg/config"
)

func TestApplySetupTargetDefaultsLocal(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.V1API.Enabled = true
	cfg.V1API.APIKey = "existing-key"

	applySetupTargetDefaults(cfg, setupTargetLocal)

	assert.Equal(t, "127.0.0.1", cfg.Gateway.Host)
	assert.False(t, cfg.V1API.Enabled)
	assert.Equal(t, "existing-key", cfg.V1API.APIKey)
}

func TestApplySetupTargetDefaultsGateway(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Workspace.Sandboxed = false
	cfg.Agents.Defaults.RestrictToWorkspace = false
	cfg.V1API.APIKey = ""

	applySetupTargetDefaults(cfg, setupTargetGateway)

	assert.Equal(t, "0.0.0.0", cfg.Gateway.Host)
	assert.True(t, cfg.V1API.Enabled)
	assert.True(t, cfg.Workspace.Sandboxed)
	assert.True(t, cfg.Agents.Defaults.RestrictToWorkspace)
	assert.NotEmpty(t, cfg.V1API.APIKey)
	assert.True(t, strings.HasPrefix(cfg.V1API.APIKey, "v1c_"))
}

func TestCollectSetupWarningsFlagsUnsafeState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = ""
	cfg.Agents.Defaults.Model = ""
	cfg.V1API.Enabled = true
	cfg.V1API.APIKey = ""
	cfg.Workspace.Sandboxed = false
	cfg.Agents.Defaults.RestrictToWorkspace = false

	warnings := collectSetupWarnings(cfg)
	joined := strings.Join(warnings, "\n")

	assert.Contains(t, joined, "AI provider and model are not configured yet.")
	assert.Contains(t, joined, "v1_api.api_key is empty")
	assert.Contains(t, joined, "Workspace security is unlocked")
}

func TestSetupSummaryLinesIncludesGatewayChannelsAndPermissions(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "gemini"
	cfg.Agents.Defaults.Model = "gemini-3.1-pro-preview"
	cfg.V1API.Enabled = true
	cfg.V1API.APIKey = "secret123"
	cfg.Channels.Telegram.Enabled = true
	cfg.Permissions.Camera = true

	lines := setupSummaryLines(cfg)
	require.NotEmpty(t, lines)

	joined := strings.Join(lines, "\n")
	assert.Contains(t, joined, "brain: gemini / gemini-3.1-pro-preview")
	assert.Contains(t, joined, "gateway: multi-device")
	assert.Contains(t, joined, "channels: Telegram")
	assert.Contains(t, joined, "permissions: camera")
}

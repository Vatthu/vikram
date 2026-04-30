package main

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/v1claw/levik/pkg/config"
)

func TestRunDoctor_ReturnsFalseWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	assert.False(t, runDoctor())
}

func TestEnabledChannelNames(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.WhatsApp.Enabled = true

	assert.Equal(t, []string{"Telegram", "WhatsApp"}, enabledChannelNames(cfg))
}

func TestDisableAllChannels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.WhatsApp.Enabled = true

	disableAllChannels(cfg)

	assert.Empty(t, enabledChannelNames(cfg))
}

func TestEnabledPermissionIDs(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Permissions.Camera = true
	cfg.Permissions.Notifications = true
	cfg.Permissions.Screen = true

	assert.Equal(t, []string{"camera", "screen", "notifications"}, enabledPermissionIDs(cfg))
}

func TestProviderCredentialStatus_BedrockDoesNotRequireAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()

	label, ready, hint := providerCredentialStatus(cfg, "bedrock")
	assert.Equal(t, "AWS credentials / profile", label)
	assert.True(t, ready)
	assert.Empty(t, hint)
}

func TestProviderCredentialStatus_GeminiRequiresCredentials(t *testing.T) {
	cfg := config.DefaultConfig()

	label, ready, hint := providerCredentialStatus(cfg, "gemini")
	assert.Empty(t, label)
	assert.False(t, ready)
	assert.Contains(t, hint, "configure")
}

func TestProviderCredentialStatus_VLLMUsesEndpointWithoutAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()

	label, ready, hint := providerCredentialStatus(cfg, "vllm")
	assert.Equal(t, "OpenAI-compatible endpoint at http://localhost:8000/v1", label)
	assert.True(t, ready)
	assert.Empty(t, hint)
}

func TestProviderCredentialStatus_OllamaUsesDefaultLocalEndpoint(t *testing.T) {
	cfg := config.DefaultConfig()

	label, ready, hint := providerCredentialStatus(cfg, "ollama")
	assert.Equal(t, "local Ollama endpoint at http://localhost:11434/v1", label)
	assert.True(t, ready)
	assert.Empty(t, hint)
}

func TestProviderConnectionHint_Ollama(t *testing.T) {
	cfg := config.DefaultConfig()

	assert.Equal(t, "Start Ollama and make sure http://localhost:11434/v1 is reachable.", providerConnectionHint(cfg, "ollama"))
}

func TestSimplifyProviderErrorFor_LocalProvider(t *testing.T) {
	err := errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")

	assert.Equal(t, "Cannot reach the configured local endpoint.", simplifyProviderErrorFor("ollama", err))
}

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLookupProviderInfo_KnownProvider(t *testing.T) {
	info, ok := lookupProviderInfo("gemini")
	assert.True(t, ok)
	assert.Equal(t, "gemini", info.id)
}

func TestLookupProviderInfo_UnknownProvider(t *testing.T) {
	_, ok := lookupProviderInfo("not-a-provider")
	assert.False(t, ok)
}

func TestDefaultProviderModel(t *testing.T) {
	assert.NotEmpty(t, defaultProviderModel("gemini"))
	assert.NotEmpty(t, defaultProviderModel("nvidia"))
	assert.Empty(t, defaultProviderModel("github_copilot"))
}

func TestSupportedProviderList_HasNoDuplicates(t *testing.T) {
	assert.Equal(t, "gemini, vertex, openai, anthropic, mistral, xai, cerebras, sambanova, github_models, bedrock, azure_openai, groq, deepseek, openrouter, nvidia, ollama, vllm", supportedProviderList())
}

func TestProviderNeedsAPIKey(t *testing.T) {
	assert.True(t, providerNeedsAPIKey("gemini"))
	assert.False(t, providerNeedsAPIKey("ollama"))
	assert.False(t, providerNeedsAPIKey("vllm"))
}

func TestDefaultProviderAPIBase(t *testing.T) {
	assert.Equal(t, "http://localhost:11434/v1", defaultProviderAPIBase("ollama"))
	assert.Equal(t, "http://localhost:8000/v1", defaultProviderAPIBase("vllm"))
	assert.Empty(t, defaultProviderAPIBase("openai"))
}

func TestDefaultGitHubCopilotTarget(t *testing.T) {
	assert.Equal(t, "copilot", defaultGitHubCopilotTarget("stdio"))
	assert.Equal(t, "localhost:4321", defaultGitHubCopilotTarget("grpc"))
}

func TestHasFlag(t *testing.T) {
	assert.True(t, hasFlag([]string{"--auto", "--skip-test"}, "--skip-test"))
	assert.True(t, hasFlag([]string{"--skip-test=true"}, "--skip-test"))
	assert.False(t, hasFlag([]string{"--skip-test=false"}, "--skip-test"))
	assert.False(t, hasFlag([]string{"--auto"}, "--skip-test"))
}

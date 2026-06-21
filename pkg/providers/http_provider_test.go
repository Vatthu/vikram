package providers

import (
	"testing"

	"github.com/Vatthu/vikram/pkg/config"
)

func TestCreateProvider_MoonshotExplicitProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "moonshot"
	cfg.Agents.Defaults.Model = "kimi-k2"
	cfg.Providers.Moonshot.APIKey = "moonshot-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider(moonshot) error = %v", err)
	}

	httpProvider, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("CreateProvider(moonshot) returned %T, want *HTTPProvider", provider)
	}
	if httpProvider.apiKey != "moonshot-key" {
		t.Fatalf("apiKey = %q, want moonshot-key", httpProvider.apiKey)
	}
	if httpProvider.apiBase != "https://api.moonshot.cn/v1" {
		t.Fatalf("apiBase = %q, want https://api.moonshot.cn/v1", httpProvider.apiBase)
	}
}

func TestCreateProvider_OllamaExplicitProviderDoesNotRequireAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "ollama"
	cfg.Agents.Defaults.Model = "llama3.2"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider(ollama) error = %v", err)
	}

	httpProvider, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("CreateProvider(ollama) returned %T, want *HTTPProvider", provider)
	}
	if httpProvider.apiKey != "" {
		t.Fatalf("apiKey = %q, want empty", httpProvider.apiKey)
	}
	if httpProvider.apiBase != "http://localhost:11434/v1" {
		t.Fatalf("apiBase = %q, want http://localhost:11434/v1", httpProvider.apiBase)
	}
}

func TestCreateProvider_VLLMExplicitProviderDoesNotRequireAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "vllm"
	cfg.Agents.Defaults.Model = "custom-local-model"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider(vllm) error = %v", err)
	}

	httpProvider, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("CreateProvider(vllm) returned %T, want *HTTPProvider", provider)
	}
	if httpProvider.apiKey != "" {
		t.Fatalf("apiKey = %q, want empty", httpProvider.apiKey)
	}
	if httpProvider.apiBase != "http://localhost:8000/v1" {
		t.Fatalf("apiBase = %q, want http://localhost:8000/v1", httpProvider.apiBase)
	}
}

func TestCreateProviderForFallback_VLLMDoesNotRequireAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Providers.VLLM.APIBase = "http://127.0.0.1:8000/v1"

	provider, err := CreateProviderForFallback(cfg, "vllm", "fake-model")
	if err != nil {
		t.Fatalf("CreateProviderForFallback(vllm) error = %v", err)
	}

	httpProvider, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("CreateProviderForFallback(vllm) returned %T, want *HTTPProvider", provider)
	}
	if httpProvider.apiKey != "" {
		t.Fatalf("apiKey = %q, want empty", httpProvider.apiKey)
	}
	if httpProvider.apiBase != "http://127.0.0.1:8000/v1" {
		t.Fatalf("apiBase = %q, want http://127.0.0.1:8000/v1", httpProvider.apiBase)
	}
}

func TestCreateProvider_GitHubCopilotIsExplicitlyUnsupported(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "github_copilot"
	cfg.Agents.Defaults.Model = "gpt-4.1"

	provider, err := CreateProvider(cfg)
	if err == nil {
		t.Fatalf("CreateProvider(github_copilot) returned provider %T, want unsupported-provider error", provider)
	}
	if got := err.Error(); got != "github_copilot provider is not supported in vikram" {
		t.Fatalf("error = %q, want explicit unsupported-provider error", got)
	}
}

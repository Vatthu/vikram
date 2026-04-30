// License: MIT

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/v1claw/levik/pkg/auth"
	"github.com/v1claw/levik/pkg/config"
)

type HTTPProvider struct {
	apiKey     string
	apiBase    string
	httpClient *http.Client
}

func NewHTTPProvider(apiKey, apiBase, proxy string) *HTTPProvider {
	client := &http.Client{
		Timeout: 120 * time.Second,
	}

	if proxy != "" {
		proxyURL, err := url.Parse(proxy)
		if err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			}
		}
	}

	return &HTTPProvider{
		apiKey:     apiKey,
		apiBase:    strings.TrimRight(apiBase, "/"),
		httpClient: client,
	}
}

func (p *HTTPProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if p.apiBase == "" {
		return nil, fmt.Errorf("API base not configured")
	}

	// Strip provider prefix from model name (e.g., moonshot/kimi-k2.5 -> kimi-k2.5,
	// openrouter/anthropic/claude-3-opus -> anthropic/claude-3-opus).
	if idx := strings.Index(model, "/"); idx != -1 {
		prefix := model[:idx]
		if prefix == "moonshot" || prefix == "nvidia" || prefix == "groq" || prefix == "ollama" || prefix == "openrouter" {
			model = model[idx+1:]
		}
	}

	requestBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
	}

	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}

	if maxTokens, ok := options["max_tokens"].(int); ok {
		lowerModel := strings.ToLower(model)
		if strings.Contains(lowerModel, "glm") || strings.Contains(lowerModel, "o1") {
			requestBody["max_completion_tokens"] = maxTokens
		} else {
			requestBody["max_tokens"] = maxTokens
		}
	}

	if temperature, ok := options["temperature"].(float64); ok {
		lowerModel := strings.ToLower(model)
		// Kimi k2 models only support temperature=1
		if strings.Contains(lowerModel, "kimi") && strings.Contains(lowerModel, "k2") {
			requestBody["temperature"] = 1.0
		} else {
			requestBody["temperature"] = temperature
		}
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.apiBase+"/chat/completions", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, formatAPIError(resp.StatusCode, body)
	}

	return p.parseResponse(body)
}

// formatAPIError turns raw API error responses into human-readable messages.
func formatAPIError(statusCode int, body []byte) error {
	// Try to extract error message from JSON response.
	// Handles both {"error": {...}} and [{"error": {...}}] (Gemini wraps in array).
	raw := bytes.TrimSpace(body)
	if len(raw) > 0 && raw[0] == '[' {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err == nil && len(arr) > 0 {
			raw = arr[0]
		}
	}

	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Code    any    `json:"code"`
		} `json:"error"`
	}

	if err := json.Unmarshal(raw, &apiErr); err == nil && apiErr.Error.Message != "" {
		msg := apiErr.Error.Message

		// Strip verbose quota details (lines starting with "* Quota exceeded...")
		if idx := strings.Index(msg, "\n*"); idx > 0 {
			msg = strings.TrimSpace(msg[:idx])
		}

		switch statusCode {
		case 429:
			return fmt.Errorf("rate limit exceeded: %s\n\n  Your free tier quota is used up. Wait a few minutes, or upgrade your plan.\n  Check usage at your provider's dashboard.", msg)
		case 401:
			return fmt.Errorf("invalid API key: Your API key was rejected.\n  Check your key in %s or re-run: levik onboard", config.ConfigPath())
		case 403:
			return fmt.Errorf("access denied: %s\n  Your API key may not have access to this model.", msg)
		case 404:
			return fmt.Errorf("model not found: %s\n  Check the model name in %s", msg, config.ConfigPath())
		default:
			return fmt.Errorf("API error (%d): %s", statusCode, msg)
		}
	}

	// Fallback: truncate long non-JSON bodies
	s := string(body)
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return fmt.Errorf("API error (%d): %s", statusCode, s)
}

func (p *HTTPProvider) parseResponse(body []byte) (*LLMResponse, error) {
	var apiResponse struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function *struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage *UsageInfo `json:"usage"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	if len(apiResponse.Choices) == 0 {
		return &LLMResponse{
			Content:      "",
			FinishReason: "stop",
		}, nil
	}

	choice := apiResponse.Choices[0]

	toolCalls := make([]ToolCall, 0, len(choice.Message.ToolCalls))
	for _, tc := range choice.Message.ToolCalls {
		arguments := make(map[string]interface{})
		name := ""

		// Handle OpenAI format with nested function object
		if tc.Type == "function" && tc.Function != nil {
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		} else if tc.Function != nil {
			// Legacy format without type field
			name = tc.Function.Name
			if tc.Function.Arguments != "" {
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &arguments); err != nil {
					arguments["raw"] = tc.Function.Arguments
				}
			}
		}

		toolCalls = append(toolCalls, ToolCall{
			ID:        tc.ID,
			Name:      name,
			Arguments: arguments,
		})
	}

	return &LLMResponse{
		Content:      choice.Message.Content,
		ToolCalls:    toolCalls,
		FinishReason: choice.FinishReason,
		Usage:        apiResponse.Usage,
	}, nil
}

func (p *HTTPProvider) GetDefaultModel() string {
	return ""
}

func providerAllowsKeylessHTTP(providerName, model string, apiBase string) bool {
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "ollama":
		return true
	case "vllm":
		return apiBase != ""
	}

	lowerModel := strings.ToLower(model)
	if strings.Contains(lowerModel, "ollama") || strings.HasPrefix(lowerModel, "ollama/") {
		return true
	}

	return apiBase != "" && providerName == ""
}

func createClaudeAuthProvider() (LLMProvider, error) {
	cred, err := auth.GetCredential("anthropic")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for anthropic. Run: levik auth login --provider anthropic")
	}
	return NewClaudeProviderWithTokenSource(cred.AccessToken, createClaudeTokenSource()), nil
}

func createCodexAuthProvider() (LLMProvider, error) {
	cred, err := auth.GetCredential("openai")
	if err != nil {
		return nil, fmt.Errorf("loading auth credentials: %w", err)
	}
	if cred == nil {
		return nil, fmt.Errorf("no credentials for openai. Run: levik auth login --provider openai")
	}
	return NewCodexProviderWithTokenSource(cred.AccessToken, cred.AccountID, createCodexTokenSource()), nil
}

// CreateProviderForFallback builds an LLMProvider targeting a specific provider by name.
// Used by the Council to switch to a different provider when the primary fails.
// Credentials are read from cfg but the provider name and model are taken from the arguments.
func CreateProviderForFallback(cfg *config.Config, providerName, model string) (LLMProvider, error) {
	if providerName == "" || model == "" {
		return nil, fmt.Errorf("council fallback: provider name and model must both be set")
	}
	pn := strings.ToLower(providerName)

	switch pn {
	case "groq":
		if cfg.Providers.Groq.APIKey != "" {
			apiBase := cfg.Providers.Groq.APIBase
			if apiBase == "" {
				apiBase = "https://api.groq.com/openai/v1"
			}
			return NewHTTPProvider(cfg.Providers.Groq.APIKey, apiBase, cfg.Providers.Groq.Proxy), nil
		}
	case "openai", "gpt":
		if cfg.Providers.OpenAI.APIKey != "" {
			apiBase := cfg.Providers.OpenAI.APIBase
			if apiBase == "" {
				apiBase = "https://api.openai.com/v1"
			}
			return NewHTTPProvider(cfg.Providers.OpenAI.APIKey, apiBase, cfg.Providers.OpenAI.Proxy), nil
		}
	case "anthropic", "claude":
		if cfg.Providers.Anthropic.APIKey != "" {
			apiBase := cfg.Providers.Anthropic.APIBase
			if apiBase == "" {
				apiBase = "https://api.anthropic.com/v1"
			}
			return NewHTTPProvider(cfg.Providers.Anthropic.APIKey, apiBase, cfg.Providers.Anthropic.Proxy), nil
		}
	case "openrouter":
		if cfg.Providers.OpenRouter.APIKey != "" {
			apiBase := cfg.Providers.OpenRouter.APIBase
			if apiBase == "" {
				apiBase = "https://openrouter.ai/api/v1"
			}
			return NewHTTPProvider(cfg.Providers.OpenRouter.APIKey, apiBase, cfg.Providers.OpenRouter.Proxy), nil
		}
	case "gemini", "google":
		if cfg.Providers.Gemini.APIKey != "" {
			apiBase := cfg.Providers.Gemini.APIBase
			if apiBase == "" {
				apiBase = "https://generativelanguage.googleapis.com/v1beta"
			}
			return NewHTTPProvider(cfg.Providers.Gemini.APIKey, apiBase, cfg.Providers.Gemini.Proxy), nil
		}
	case "zhipu", "glm":
		if cfg.Providers.Zhipu.APIKey != "" {
			apiBase := cfg.Providers.Zhipu.APIBase
			if apiBase == "" {
				apiBase = "https://open.bigmodel.cn/api/paas/v4"
			}
			return NewHTTPProvider(cfg.Providers.Zhipu.APIKey, apiBase, cfg.Providers.Zhipu.Proxy), nil
		}
	case "deepseek":
		if cfg.Providers.DeepSeek.APIKey != "" {
			apiBase := cfg.Providers.DeepSeek.APIBase
			if apiBase == "" {
				apiBase = "https://api.deepseek.com/v1"
			}
			return NewHTTPProvider(cfg.Providers.DeepSeek.APIKey, apiBase, cfg.Providers.DeepSeek.Proxy), nil
		}
	case "nvidia":
		if cfg.Providers.Nvidia.APIKey != "" {
			apiBase := cfg.Providers.Nvidia.APIBase
			if apiBase == "" {
				apiBase = "https://integrate.api.nvidia.com/v1"
			}
			return NewHTTPProvider(cfg.Providers.Nvidia.APIKey, apiBase, cfg.Providers.Nvidia.Proxy), nil
		}
	case "ollama":
		if cfg.Providers.Ollama.APIBase != "" {
			return NewHTTPProvider(cfg.Providers.Ollama.APIKey, cfg.Providers.Ollama.APIBase, cfg.Providers.Ollama.Proxy), nil
		}
	case "vllm":
		apiBase := cfg.Providers.VLLM.APIBase
		if apiBase != "" {
			return NewHTTPProvider(cfg.Providers.VLLM.APIKey, apiBase, cfg.Providers.VLLM.Proxy), nil
		}
	case "vertex", "vertex_ai", "vertexai":
		v := cfg.Providers.Vertex
		if v.ProjectID != "" {
			return NewVertexProvider(v.ProjectID, v.Location, v.APIKey, v.Grounding), nil
		}
	case "moonshot", "kimi":
		if cfg.Providers.Moonshot.APIKey != "" {
			apiBase := cfg.Providers.Moonshot.APIBase
			if apiBase == "" {
				apiBase = "https://api.moonshot.cn/v1"
			}
			return NewHTTPProvider(cfg.Providers.Moonshot.APIKey, apiBase, cfg.Providers.Moonshot.Proxy), nil
		}
	case "mistral":
		if cfg.Providers.Mistral.APIKey != "" {
			apiBase := cfg.Providers.Mistral.APIBase
			if apiBase == "" {
				apiBase = "https://api.mistral.ai/v1"
			}
			return NewHTTPProvider(cfg.Providers.Mistral.APIKey, apiBase, cfg.Providers.Mistral.Proxy), nil
		}
	case "xai", "grok":
		if cfg.Providers.XAI.APIKey != "" {
			apiBase := cfg.Providers.XAI.APIBase
			if apiBase == "" {
				apiBase = "https://api.x.ai/v1"
			}
			return NewHTTPProvider(cfg.Providers.XAI.APIKey, apiBase, cfg.Providers.XAI.Proxy), nil
		}
	case "cerebras":
		if cfg.Providers.Cerebras.APIKey != "" {
			apiBase := cfg.Providers.Cerebras.APIBase
			if apiBase == "" {
				apiBase = "https://api.cerebras.ai/v1"
			}
			return NewHTTPProvider(cfg.Providers.Cerebras.APIKey, apiBase, cfg.Providers.Cerebras.Proxy), nil
		}
	case "sambanova":
		if cfg.Providers.SambaNova.APIKey != "" {
			apiBase := cfg.Providers.SambaNova.APIBase
			if apiBase == "" {
				apiBase = "https://api.sambanova.ai/v1"
			}
			return NewHTTPProvider(cfg.Providers.SambaNova.APIKey, apiBase, cfg.Providers.SambaNova.Proxy), nil
		}
	case "github_models":
		if cfg.Providers.GitHubModels.APIKey != "" {
			apiBase := cfg.Providers.GitHubModels.APIBase
			if apiBase == "" {
				apiBase = "https://models.inference.ai.azure.com"
			}
			return NewHTTPProvider(cfg.Providers.GitHubModels.APIKey, apiBase, cfg.Providers.GitHubModels.Proxy), nil
		}
	case "claude-cli", "claude-code", "claudecode":
		return nil, fmt.Errorf("council fallback: claude-cli provider is not supported in levik")
	case "codex-cli", "codex-code":
		return nil, fmt.Errorf("council fallback: codex-cli provider is not supported in levik")
	}

	return nil, fmt.Errorf("council fallback: no API key configured for provider %q (model: %s)", providerName, model)
}

func CreateProvider(cfg *config.Config) (LLMProvider, error) {
	model := cfg.Agents.Defaults.Model
	providerName := strings.ToLower(cfg.Agents.Defaults.Provider)

	var apiKey, apiBase, proxy string

	lowerModel := strings.ToLower(model)

	// First, try to use explicitly configured provider
	if providerName != "" {
		switch providerName {
		case "groq":
			if cfg.Providers.Groq.APIKey != "" {
				apiKey = cfg.Providers.Groq.APIKey
				apiBase = cfg.Providers.Groq.APIBase
				if apiBase == "" {
					apiBase = "https://api.groq.com/openai/v1"
				}
			}
		case "openai", "gpt":
			if cfg.Providers.OpenAI.APIKey != "" || cfg.Providers.OpenAI.AuthMethod != "" {
				if cfg.Providers.OpenAI.AuthMethod == "codex-cli" {
					return nil, fmt.Errorf("codex-cli auth method is not supported in levik")
				}
				if cfg.Providers.OpenAI.AuthMethod == "oauth" || cfg.Providers.OpenAI.AuthMethod == "token" {
					return createCodexAuthProvider()
				}
				apiKey = cfg.Providers.OpenAI.APIKey
				apiBase = cfg.Providers.OpenAI.APIBase
				if apiBase == "" {
					apiBase = "https://api.openai.com/v1"
				}
			}
		case "anthropic", "claude":
			if cfg.Providers.Anthropic.APIKey != "" || cfg.Providers.Anthropic.AuthMethod != "" {
				if cfg.Providers.Anthropic.AuthMethod == "oauth" || cfg.Providers.Anthropic.AuthMethod == "token" {
					return createClaudeAuthProvider()
				}
				apiKey = cfg.Providers.Anthropic.APIKey
				apiBase = cfg.Providers.Anthropic.APIBase
				if apiBase == "" {
					apiBase = "https://api.anthropic.com/v1"
				}
			}
		case "openrouter":
			if cfg.Providers.OpenRouter.APIKey != "" {
				apiKey = cfg.Providers.OpenRouter.APIKey
				if cfg.Providers.OpenRouter.APIBase != "" {
					apiBase = cfg.Providers.OpenRouter.APIBase
				} else {
					apiBase = "https://openrouter.ai/api/v1"
				}
			}
		case "zhipu", "glm":
			if cfg.Providers.Zhipu.APIKey != "" {
				apiKey = cfg.Providers.Zhipu.APIKey
				apiBase = cfg.Providers.Zhipu.APIBase
				if apiBase == "" {
					apiBase = "https://open.bigmodel.cn/api/paas/v4"
				}
			}
		case "moonshot":
			if cfg.Providers.Moonshot.APIKey != "" {
				apiKey = cfg.Providers.Moonshot.APIKey
				apiBase = cfg.Providers.Moonshot.APIBase
				if apiBase == "" {
					apiBase = "https://api.moonshot.cn/v1"
				}
			}
		case "gemini", "google":
			if cfg.Providers.Gemini.APIKey != "" {
				apiKey = cfg.Providers.Gemini.APIKey
				apiBase = cfg.Providers.Gemini.APIBase
				if apiBase == "" {
					apiBase = "https://generativelanguage.googleapis.com/v1beta"
				}
			}
		case "vllm":
			apiKey = cfg.Providers.VLLM.APIKey
			apiBase = cfg.Providers.VLLM.APIBase
			if apiBase == "" {
				apiBase = "http://localhost:8000/v1"
			}
		case "shengsuanyun":
			if cfg.Providers.ShengSuanYun.APIKey != "" {
				apiKey = cfg.Providers.ShengSuanYun.APIKey
				apiBase = cfg.Providers.ShengSuanYun.APIBase
				if apiBase == "" {
					apiBase = "https://router.shengsuanyun.com/api/v1"
				}
			}
		case "claude-cli", "claudecode", "claude-code":
			return nil, fmt.Errorf("claude-cli provider is not supported in levik")
		case "codex-cli", "codex-code":
			return nil, fmt.Errorf("codex-cli provider is not supported in levik")

		// ── Enterprise / cloud-native providers ──────────────────────────
		case "vertex", "vertex_ai", "vertexai":
			v := cfg.Providers.Vertex
			if v.ProjectID == "" {
				return nil, fmt.Errorf("vertex: project_id is required — set providers.vertex.project_id in config")
			}
			return NewVertexProvider(v.ProjectID, v.Location, v.APIKey, v.Grounding), nil

		case "bedrock", "aws_bedrock", "aws":
			b := cfg.Providers.Bedrock
			p, err := NewBedrockProvider(b.Region, b.AccessKeyID, b.SecretAccessKey, b.SessionToken, b.Profile)
			if err != nil {
				return nil, err
			}
			return p, nil

		case "azure_openai", "azure", "azureopenai":
			a := cfg.Providers.AzureOpenAI
			if a.Endpoint == "" || a.Deployment == "" {
				return nil, fmt.Errorf("azure_openai: endpoint and deployment are required — set providers.azure_openai.endpoint and .deployment in config")
			}
			return NewAzureOpenAIProvider(a.Endpoint, a.Deployment, a.APIKey, a.APIVersion), nil
		case "deepseek":
			if cfg.Providers.DeepSeek.APIKey != "" {
				apiKey = cfg.Providers.DeepSeek.APIKey
				apiBase = cfg.Providers.DeepSeek.APIBase
				if apiBase == "" {
					apiBase = "https://api.deepseek.com/v1"
				}
				if model != "deepseek-chat" && model != "deepseek-reasoner" {
					model = "deepseek-chat"
				}
			}
		case "ollama":
			apiKey = cfg.Providers.Ollama.APIKey
			apiBase = cfg.Providers.Ollama.APIBase
			proxy = cfg.Providers.Ollama.Proxy
			if apiBase == "" {
				apiBase = "http://localhost:11434/v1"
			}
		case "github_copilot", "copilot":
			return nil, fmt.Errorf("github_copilot provider is not supported in levik")

		case "mistral":
			if cfg.Providers.Mistral.APIKey != "" {
				apiKey = cfg.Providers.Mistral.APIKey
				apiBase = cfg.Providers.Mistral.APIBase
				if apiBase == "" {
					apiBase = "https://api.mistral.ai/v1"
				}
			}
		case "xai", "grok":
			if cfg.Providers.XAI.APIKey != "" {
				apiKey = cfg.Providers.XAI.APIKey
				apiBase = cfg.Providers.XAI.APIBase
				if apiBase == "" {
					apiBase = "https://api.x.ai/v1"
				}
			}
		case "cerebras":
			if cfg.Providers.Cerebras.APIKey != "" {
				apiKey = cfg.Providers.Cerebras.APIKey
				apiBase = cfg.Providers.Cerebras.APIBase
				if apiBase == "" {
					apiBase = "https://api.cerebras.ai/v1"
				}
			}
		case "sambanova":
			if cfg.Providers.SambaNova.APIKey != "" {
				apiKey = cfg.Providers.SambaNova.APIKey
				apiBase = cfg.Providers.SambaNova.APIBase
				if apiBase == "" {
					apiBase = "https://api.sambanova.ai/v1"
				}
			}
		case "github_models":
			if cfg.Providers.GitHubModels.APIKey != "" {
				apiKey = cfg.Providers.GitHubModels.APIKey
				apiBase = cfg.Providers.GitHubModels.APIBase
				if apiBase == "" {
					apiBase = "https://models.inference.ai.azure.com"
				}
			}

		}

	}

	// Fallback: detect provider from model name
	if apiKey == "" && apiBase == "" {
		switch {
		case (strings.Contains(lowerModel, "kimi") || strings.Contains(lowerModel, "moonshot") || strings.HasPrefix(model, "moonshot/")) && cfg.Providers.Moonshot.APIKey != "":
			apiKey = cfg.Providers.Moonshot.APIKey
			apiBase = cfg.Providers.Moonshot.APIBase
			proxy = cfg.Providers.Moonshot.Proxy
			if apiBase == "" {
				apiBase = "https://api.moonshot.cn/v1"
			}

		case strings.HasPrefix(model, "openrouter/") || strings.HasPrefix(model, "anthropic/") || strings.HasPrefix(model, "openai/") || strings.HasPrefix(model, "meta-llama/") || strings.HasPrefix(model, "deepseek/") || strings.HasPrefix(model, "google/"):
			apiKey = cfg.Providers.OpenRouter.APIKey
			proxy = cfg.Providers.OpenRouter.Proxy
			if cfg.Providers.OpenRouter.APIBase != "" {
				apiBase = cfg.Providers.OpenRouter.APIBase
			} else {
				apiBase = "https://openrouter.ai/api/v1"
			}

		case (strings.Contains(lowerModel, "claude") || strings.HasPrefix(model, "anthropic/")) && (cfg.Providers.Anthropic.APIKey != "" || cfg.Providers.Anthropic.AuthMethod != ""):
			if cfg.Providers.Anthropic.AuthMethod == "oauth" || cfg.Providers.Anthropic.AuthMethod == "token" {
				return createClaudeAuthProvider()
			}
			apiKey = cfg.Providers.Anthropic.APIKey
			apiBase = cfg.Providers.Anthropic.APIBase
			proxy = cfg.Providers.Anthropic.Proxy
			if apiBase == "" {
				apiBase = "https://api.anthropic.com/v1"
			}

		case (strings.Contains(lowerModel, "gpt") || strings.HasPrefix(model, "openai/")) && (cfg.Providers.OpenAI.APIKey != "" || cfg.Providers.OpenAI.AuthMethod != ""):
			if cfg.Providers.OpenAI.AuthMethod == "oauth" || cfg.Providers.OpenAI.AuthMethod == "token" {
				return createCodexAuthProvider()
			}
			apiKey = cfg.Providers.OpenAI.APIKey
			apiBase = cfg.Providers.OpenAI.APIBase
			proxy = cfg.Providers.OpenAI.Proxy
			if apiBase == "" {
				apiBase = "https://api.openai.com/v1"
			}

		case (strings.Contains(lowerModel, "gemini") || strings.HasPrefix(model, "google/")) && cfg.Providers.Gemini.APIKey != "":
			apiKey = cfg.Providers.Gemini.APIKey
			apiBase = cfg.Providers.Gemini.APIBase
			proxy = cfg.Providers.Gemini.Proxy
			if apiBase == "" {
				apiBase = "https://generativelanguage.googleapis.com/v1beta"
			}

		case (strings.Contains(lowerModel, "glm") || strings.Contains(lowerModel, "zhipu") || strings.Contains(lowerModel, "zai")) && cfg.Providers.Zhipu.APIKey != "":
			apiKey = cfg.Providers.Zhipu.APIKey
			apiBase = cfg.Providers.Zhipu.APIBase
			proxy = cfg.Providers.Zhipu.Proxy
			if apiBase == "" {
				apiBase = "https://open.bigmodel.cn/api/paas/v4"
			}

		case (strings.Contains(lowerModel, "groq") || strings.HasPrefix(model, "groq/")) && cfg.Providers.Groq.APIKey != "":
			apiKey = cfg.Providers.Groq.APIKey
			apiBase = cfg.Providers.Groq.APIBase
			proxy = cfg.Providers.Groq.Proxy
			if apiBase == "" {
				apiBase = "https://api.groq.com/openai/v1"
			}

		case (strings.Contains(lowerModel, "nvidia") || strings.HasPrefix(model, "nvidia/")) && cfg.Providers.Nvidia.APIKey != "":
			apiKey = cfg.Providers.Nvidia.APIKey
			apiBase = cfg.Providers.Nvidia.APIBase
			proxy = cfg.Providers.Nvidia.Proxy
			if apiBase == "" {
				apiBase = "https://integrate.api.nvidia.com/v1"
			}
		case strings.Contains(lowerModel, "ollama") || strings.HasPrefix(model, "ollama/"):
			apiKey = cfg.Providers.Ollama.APIKey
			apiBase = cfg.Providers.Ollama.APIBase
			proxy = cfg.Providers.Ollama.Proxy
			if apiBase == "" {
				apiBase = "http://localhost:11434/v1"
			}
		case cfg.Providers.VLLM.APIBase != "":
			apiKey = cfg.Providers.VLLM.APIKey
			apiBase = cfg.Providers.VLLM.APIBase
			proxy = cfg.Providers.VLLM.Proxy

		default:
			if cfg.Providers.OpenRouter.APIKey != "" {
				apiKey = cfg.Providers.OpenRouter.APIKey
				proxy = cfg.Providers.OpenRouter.Proxy
				if cfg.Providers.OpenRouter.APIBase != "" {
					apiBase = cfg.Providers.OpenRouter.APIBase
				} else {
					apiBase = "https://openrouter.ai/api/v1"
				}
			} else {
				return nil, fmt.Errorf("no API key configured for model: %s", model)
			}
		}
	}

	if apiKey == "" && !strings.HasPrefix(model, "bedrock/") && !providerAllowsKeylessHTTP(providerName, model, apiBase) {
		return nil, fmt.Errorf("no API key configured for provider (model: %s)", model)
	}

	if apiBase == "" {
		return nil, fmt.Errorf("no API base configured for provider (model: %s)", model)
	}

	return NewHTTPProvider(apiKey, apiBase, proxy), nil
}

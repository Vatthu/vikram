// Vikram — Azure OpenAI provider
// Uses private Azure OpenAI endpoints with deployment-based model selection.
// Auth: api-key header (or Azure AD — set auth_method: "azure_ad" and api_key to the AD token).

package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const azureDefaultAPIVersion = "2024-10-21"

// AzureOpenAIProvider implements LLMProvider against a private Azure OpenAI deployment.
type AzureOpenAIProvider struct {
	endpoint   string // e.g. https://mycompany.openai.azure.com
	deployment string // deployment name (maps to a model version)
	apiKey     string
	apiVersion string
	httpClient *http.Client
}

// NewAzureOpenAIProvider creates an Azure OpenAI provider.
// deployment is the name of the deployment (e.g. "gpt-4o-prod").
// apiVersion defaults to "2024-10-21" if empty.
func NewAzureOpenAIProvider(endpoint, deployment, apiKey, apiVersion string) *AzureOpenAIProvider {
	endpoint = strings.TrimRight(endpoint, "/")
	if apiVersion == "" {
		apiVersion = azureDefaultAPIVersion
	}
	return &AzureOpenAIProvider{
		endpoint:   endpoint,
		deployment: deployment,
		apiKey:     apiKey,
		apiVersion: apiVersion,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

func (p *AzureOpenAIProvider) GetDefaultModel() string {
	// For Azure the model is the deployment name, not a model ID.
	return p.deployment
}

// Chat implements LLMProvider using the Azure OpenAI chat completions endpoint.
func (p *AzureOpenAIProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	// For Azure, model comes from the deployment URL, not the request body.
	// If caller passes a model that matches the deployment, use it; otherwise use deployment.
	if model == "" || model == p.deployment {
		model = p.deployment
	}

	url := fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		p.endpoint, p.deployment, p.apiVersion)

	requestBody := map[string]interface{}{
		// Azure ignores "model" in the body (it's set by the deployment), but
		// including it helps with logging and is harmless.
		"model":    model,
		"messages": messages,
	}

	if len(tools) > 0 {
		requestBody["tools"] = tools
		requestBody["tool_choice"] = "auto"
	}
	if v, ok := options["max_tokens"].(int); ok {
		requestBody["max_tokens"] = v
	}
	if v, ok := options["temperature"].(float64); ok {
		requestBody["temperature"] = v
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("azure: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Azure uses "api-key" header, not "Authorization: Bearer".
	req.Header.Set("api-key", p.apiKey)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("azure: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("azure: read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseAzureError(resp.StatusCode, raw)
	}

	// Azure uses the standard OpenAI response format — reuse the existing parser.
	hp := &HTTPProvider{}
	return hp.parseResponse(raw)
}

func parseAzureError(status int, body []byte) error {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		switch status {
		case 401:
			return fmt.Errorf("azure: invalid api-key: %s\n  Check your Azure OpenAI api_key in config", apiErr.Error.Message)
		case 404:
			return fmt.Errorf("azure: deployment or resource not found: %s\n  Check endpoint, deployment name, and api_version in config", apiErr.Error.Message)
		case 429:
			return fmt.Errorf("azure: rate limit / quota exceeded: %s\n  Increase quota in Azure OpenAI Studio or wait", apiErr.Error.Message)
		default:
			return fmt.Errorf("azure: API error (%d): %s", status, apiErr.Error.Message)
		}
	}
	return fmt.Errorf("azure: API error (%d): %s", status, string(body))
}

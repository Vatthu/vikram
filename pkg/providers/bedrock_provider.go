// Vikram — AWS Bedrock provider
// Uses the Bedrock Converse API for a unified interface across Claude, Llama, Titan, and more.
// Auth: AWS credential chain — config file → env vars → ~/.aws/credentials → IAM role.

package providers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BedrockProvider implements LLMProvider using the Bedrock Converse API.
type BedrockProvider struct {
	region          string
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	httpClient      *http.Client
}

// NewBedrockProvider creates a Bedrock provider.
// Credential resolution order: explicit params → AWS env vars → ~/.aws/credentials.
func NewBedrockProvider(region, accessKeyID, secretAccessKey, sessionToken, profile string) (*BedrockProvider, error) {
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
		if region == "" {
			region = os.Getenv("AWS_REGION")
		}
		if region == "" {
			region = "us-east-1"
		}
	}

	// Explicit creds take priority.
	if accessKeyID != "" && secretAccessKey != "" {
		return &BedrockProvider{
			region: region, accessKeyID: accessKeyID,
			secretAccessKey: secretAccessKey, sessionToken: sessionToken,
			httpClient: &http.Client{Timeout: 180 * time.Second},
		}, nil
	}

	// Env vars.
	if k := os.Getenv("AWS_ACCESS_KEY_ID"); k != "" {
		return &BedrockProvider{
			region: region, accessKeyID: k,
			secretAccessKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
			sessionToken:    os.Getenv("AWS_SESSION_TOKEN"),
			httpClient:      &http.Client{Timeout: 180 * time.Second},
		}, nil
	}

	// ~/.aws/credentials file.
	if profile == "" {
		profile = os.Getenv("AWS_PROFILE")
	}
	if profile == "" {
		profile = "default"
	}
	kid, secret, session, err := awsCredsFromFile(profile)
	if err != nil {
		return nil, fmt.Errorf("bedrock: no AWS credentials found (set AWS_ACCESS_KEY_ID or configure ~/.aws/credentials): %w", err)
	}
	return &BedrockProvider{
		region: region, accessKeyID: kid,
		secretAccessKey: secret, sessionToken: session,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}, nil
}

func (p *BedrockProvider) GetDefaultModel() string {
	return "anthropic.claude-sonnet-4-5-20250929-v2:0"
}

// Chat implements LLMProvider using Bedrock's Converse API.
func (p *BedrockProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if model == "" {
		model = p.GetDefaultModel()
	}
	// Allow short aliases: claude-3-5-sonnet → anthropic.claude-3-5-sonnet-*
	model = resolveBedrockModel(model)

	endpoint := fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com/model/%s/converse", p.region, model)
	body, err := p.buildConverseRequest(messages, tools, options)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if err := p.signRequest(req, body); err != nil {
		return nil, fmt.Errorf("bedrock: sign request: %w", err)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bedrock: request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, parseBedrockError(resp.StatusCode, raw)
	}
	return parseBedrockResponse(raw)
}

// --- Request building ---

func (p *BedrockProvider) buildConverseRequest(messages []Message, tools []ToolDefinition, options map[string]interface{}) ([]byte, error) {
	bedrockMessages, systemText := convertMessagesToBedrock(messages)

	req := map[string]interface{}{
		"messages": bedrockMessages,
	}

	if systemText != "" {
		req["system"] = []map[string]string{{"text": systemText}}
	}

	if len(tools) > 0 {
		toolSpecs := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			toolSpecs = append(toolSpecs, map[string]interface{}{
				"toolSpec": map[string]interface{}{
					"name":        t.Function.Name,
					"description": t.Function.Description,
					"inputSchema": map[string]interface{}{"json": t.Function.Parameters},
				},
			})
		}
		req["toolConfig"] = map[string]interface{}{
			"tools":      toolSpecs,
			"toolChoice": map[string]interface{}{"auto": map[string]interface{}{}},
		}
	}

	infCfg := map[string]interface{}{}
	if v, ok := options["max_tokens"].(int); ok {
		infCfg["maxTokens"] = v
	}
	if v, ok := options["temperature"].(float64); ok {
		infCfg["temperature"] = v
	}
	if len(infCfg) > 0 {
		req["inferenceConfig"] = infCfg
	}

	return json.Marshal(req)
}

func convertMessagesToBedrock(messages []Message) ([]map[string]interface{}, string) {
	var systemText string
	var result []map[string]interface{}

	for _, m := range messages {
		switch m.Role {
		case "system":
			systemText = m.Content

		case "tool":
			result = append(result, map[string]interface{}{
				"role": "user",
				"content": []map[string]interface{}{{
					"toolResult": map[string]interface{}{
						"toolUseId": m.ToolCallID,
						"content":   []map[string]string{{"text": m.Content}},
					},
				}},
			})

		case "assistant":
			var content []map[string]interface{}
			if m.Content != "" {
				content = append(content, map[string]interface{}{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				name := tc.Name
				if tc.Function != nil {
					name = tc.Function.Name
				}
				input := tc.Arguments
				if input == nil && tc.Function != nil {
					json.Unmarshal([]byte(tc.Function.Arguments), &input) //nolint:errcheck
				}
				content = append(content, map[string]interface{}{
					"toolUse": map[string]interface{}{
						"toolUseId": tc.ID,
						"name":      name,
						"input":     input,
					},
				})
			}
			if len(content) > 0 {
				result = append(result, map[string]interface{}{"role": "assistant", "content": content})
			}

		default: // user
			result = append(result, map[string]interface{}{
				"role":    "user",
				"content": []map[string]interface{}{{"text": m.Content}},
			})
		}
	}
	return result, systemText
}

// --- Response parsing ---

func parseBedrockResponse(body []byte) (*LLMResponse, error) {
	var resp struct {
		Output struct {
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Text    string `json:"text"`
					ToolUse *struct {
						ToolUseID string                 `json:"toolUseId"`
						Name      string                 `json:"name"`
						Input     map[string]interface{} `json:"input"`
					} `json:"toolUse"`
				} `json:"content"`
			} `json:"message"`
		} `json:"output"`
		StopReason string `json:"stopReason"`
		Usage      struct {
			InputTokens  int `json:"inputTokens"`
			OutputTokens int `json:"outputTokens"`
			TotalTokens  int `json:"totalTokens"`
		} `json:"usage"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bedrock: failed to parse response: %w\nraw: %s", err, string(body))
	}

	result := &LLMResponse{
		FinishReason: strings.ToLower(resp.StopReason),
		Usage: &UsageInfo{
			PromptTokens:     resp.Usage.InputTokens,
			CompletionTokens: resp.Usage.OutputTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	for _, c := range resp.Output.Message.Content {
		if c.Text != "" {
			result.Content += c.Text
		}
		if c.ToolUse != nil {
			argsJSON, _ := json.Marshal(c.ToolUse.Input)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   c.ToolUse.ToolUseID,
				Type: "function",
				Name: c.ToolUse.Name,
				Function: &FunctionCall{
					Name:      c.ToolUse.Name,
					Arguments: string(argsJSON),
				},
				Arguments: c.ToolUse.Input,
			})
		}
	}

	return result, nil
}

func parseBedrockError(status int, body []byte) error {
	var apiErr struct {
		Message string `json:"message"`
		Type    string `json:"__type"`
	}
	msg := string(body)
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		msg = apiErr.Message
	}
	switch status {
	case 400:
		return fmt.Errorf("bedrock: bad request: %s", msg)
	case 401, 403:
		return fmt.Errorf("bedrock: access denied: %s\n  Check your AWS credentials and IAM permissions for bedrock:InvokeModel", msg)
	case 404:
		return fmt.Errorf("bedrock: model not found: %s\n  Check the model ID and that it is enabled in your AWS account region", msg)
	case 429:
		return fmt.Errorf("bedrock: throttled: %s\n  Request Bedrock quota increases in the AWS Service Quotas console", msg)
	default:
		return fmt.Errorf("bedrock: API error (%d): %s", status, msg)
	}
}

// resolveBedrockModel maps short aliases to full Bedrock model IDs.
func resolveBedrockModel(model string) string {
	aliases := map[string]string{
		"claude-3-5-sonnet": "anthropic.claude-3-5-sonnet-20241022-v2:0",
		"claude-3-5-haiku":  "anthropic.claude-3-5-haiku-20241022-v1:0",
		"claude-3-opus":     "anthropic.claude-3-opus-20240229-v1:0",
		"claude-sonnet-4-5": "anthropic.claude-sonnet-4-5-20250929-v2:0",
		"llama-3-3-70b":     "meta.llama3-3-70b-instruct-v1:0",
		"llama-3-1-405b":    "meta.llama3-1-405b-instruct-v1:0",
		"llama-3-1-70b":     "meta.llama3-1-70b-instruct-v1:0",
		"nova-pro":          "amazon.nova-pro-v1:0",
		"nova-lite":         "amazon.nova-lite-v1:0",
		"nova-micro":        "amazon.nova-micro-v1:0",
		"mistral-large":     "mistral.mistral-large-2402-v1:0",
	}
	if full, ok := aliases[strings.ToLower(model)]; ok {
		return full
	}
	return model
}

// --- AWS Signature V4 ---

// signRequest adds the AWS Signature V4 Authorization header to req.
func (p *BedrockProvider) signRequest(req *http.Request, body []byte) error {
	t := time.Now().UTC()
	date := t.Format("20060102")
	datetime := t.Format("20060102T150405Z")
	service := "bedrock"

	req.Header.Set("X-Amz-Date", datetime)
	if p.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", p.sessionToken)
	}

	bodyHash := hashSHA256(body)

	// Canonical request.
	signedHeaders := "content-type;host;x-amz-date"
	if p.sessionToken != "" {
		signedHeaders += ";x-amz-security-token"
	}

	canonicalHeaders := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.URL.Host + "\n" +
		"x-amz-date:" + datetime + "\n"
	if p.sessionToken != "" {
		canonicalHeaders += "x-amz-security-token:" + p.sessionToken + "\n"
	}

	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.Path,
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	// String to sign.
	credentialScope := strings.Join([]string{date, p.region, service, "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		datetime,
		credentialScope,
		hashSHA256([]byte(canonicalRequest)),
	}, "\n")

	// Signing key.
	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+p.secretAccessKey), []byte(date)),
				[]byte(p.region),
			),
			[]byte(service),
		),
		[]byte("aws4_request"),
	)

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		p.accessKeyID, credentialScope, signedHeaders, signature,
	))
	return nil
}

func hashSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// --- AWS credentials file ---

func awsCredsFromFile(profile string) (keyID, secret, session string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", "", err
	}
	data, err := os.ReadFile(filepath.Join(home, ".aws", "credentials"))
	if err != nil {
		return "", "", "", fmt.Errorf("read ~/.aws/credentials: %w", err)
	}

	target := "[" + profile + "]"
	inProfile := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inProfile = line == target
			continue
		}
		if !inProfile {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.TrimSpace(parts[1])
		switch k {
		case "aws_access_key_id":
			keyID = v
		case "aws_secret_access_key":
			secret = v
		case "aws_session_token":
			session = v
		}
	}

	if keyID == "" || secret == "" {
		return "", "", "", fmt.Errorf("profile %q not found or missing credentials", profile)
	}
	return keyID, secret, session, nil
}

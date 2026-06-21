// Vikram — Vertex AI provider
// Uses the native Google Cloud aiplatform generateContent API.
// Auth: gcloud ADC (gcloud auth application-default login) or GOOGLE_APPLICATION_CREDENTIALS SA JSON.
// Supports Google Search grounding when cfg.Providers.Vertex.Grounding == true.

package providers

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/logger"
)

// VertexProvider implements LLMProvider against the Vertex AI REST API.
type VertexProvider struct {
	projectID  string
	location   string
	apiKey     string // when set, uses API key query auth instead of OAuth2
	grounding  bool
	httpClient *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewVertexProvider creates a Vertex AI provider.
// projectID and location are required. apiKey is optional.
func NewVertexProvider(projectID, location, apiKey string, grounding bool) *VertexProvider {
	if location == "" {
		location = "us-central1"
	}
	return &VertexProvider{
		projectID:  projectID,
		apiKey:     apiKey,
		location:   location,
		grounding:  grounding,
		httpClient: &http.Client{Timeout: 180 * time.Second},
	}
}

func (p *VertexProvider) GetDefaultModel() string {
	return "gemini-3.1-pro-preview"
}

// accessToken returns a valid OAuth2 access token, refreshing if necessary.
func (p *VertexProvider) accessToken(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cachedToken != "" && time.Now().Before(p.tokenExpiry.Add(-30*time.Second)) {
		return p.cachedToken, nil
	}

	// 1. Try GOOGLE_APPLICATION_CREDENTIALS service account JSON.
	if saPath := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); saPath != "" {
		tok, expiry, err := tokenFromServiceAccount(ctx, saPath)
		if err == nil {
			p.cachedToken = tok
			p.tokenExpiry = expiry
			return tok, nil
		}
		logger.WarnCF("vertex", "SA JSON auth failed, trying gcloud", map[string]interface{}{"error": err.Error()})
	}

	// 2. Fall back to gcloud subprocess.
	tok, err := tokenFromGCloud(ctx)
	if err != nil {
		return "", fmt.Errorf("vertex auth: %w (run: gcloud auth application-default login)", err)
	}
	p.cachedToken = tok
	p.tokenExpiry = time.Now().Add(58 * time.Minute) // gcloud tokens last 1 hour
	return tok, nil
}

// Chat implements LLMProvider.
func (p *VertexProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	if model == "" {
		model = p.GetDefaultModel()
	}

	endpoint := fmt.Sprintf(
		"https://%s-aiplatform.googleapis.com/v1/projects/%s/locations/%s/publishers/google/models/%s:generateContent",
		p.location, p.projectID, p.location, model,
	)

	// API key goes in the URL query. Falls back to OAuth2 bearer header.
	if p.apiKey != "" {
		endpoint += "?key=" + url.QueryEscape(p.apiKey)
	}

	body, err := p.buildRequest(messages, tools, options)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	if p.apiKey == "" {
		token, err := p.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vertex request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("vertex: failed reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseVertexError(resp.StatusCode, raw)
	}

	return parseVertexResponse(raw)
}

// --- Request building ---

func (p *VertexProvider) buildRequest(messages []Message, tools []ToolDefinition, options map[string]interface{}) ([]byte, error) {
	contents, systemInstruction := convertMessagesToVertex(messages)

	req := map[string]interface{}{
		"contents": contents,
	}

	if systemInstruction != "" {
		req["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": systemInstruction}},
		}
	}

	// Tool declarations + optional grounding.
	var toolBlocks []map[string]interface{}
	if len(tools) > 0 {
		decls := make([]map[string]interface{}, 0, len(tools))
		for _, t := range tools {
			decls = append(decls, map[string]interface{}{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			})
		}
		toolBlocks = append(toolBlocks, map[string]interface{}{
			"functionDeclarations": decls,
		})
	}
	if p.grounding {
		toolBlocks = append(toolBlocks, map[string]interface{}{
			"googleSearch": map[string]interface{}{},
		})
	}
	if len(toolBlocks) > 0 {
		req["tools"] = toolBlocks
	}

	genCfg := map[string]interface{}{}
	if v, ok := options["max_tokens"].(int); ok {
		genCfg["maxOutputTokens"] = v
	}
	if v, ok := options["temperature"].(float64); ok {
		genCfg["temperature"] = v
	}
	if len(genCfg) > 0 {
		req["generationConfig"] = genCfg
	}

	return json.Marshal(req)
}

// convertMessagesToVertex converts OpenAI-format messages to Vertex contents list.
// System messages are extracted and returned as the systemInstruction string.
func convertMessagesToVertex(messages []Message) ([]map[string]interface{}, string) {
	var systemText string
	var contents []map[string]interface{}

	for _, m := range messages {
		switch m.Role {
		case "system":
			systemText = m.Content

		case "tool":
			// Tool result — wrap as functionResponse in a user turn.
			var resultVal interface{} = m.Content
			var parsed map[string]interface{}
			if json.Unmarshal([]byte(m.Content), &parsed) == nil {
				resultVal = parsed
			}
			contents = append(contents, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{{
					"functionResponse": map[string]interface{}{
						"name":     m.ToolCallID,
						"response": map[string]interface{}{"content": resultVal},
					},
				}},
			})

		case "assistant":
			var parts []map[string]interface{}
			if m.Content != "" {
				parts = append(parts, map[string]interface{}{"text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				name := tc.Name
				if tc.Function != nil {
					name = tc.Function.Name
				}
				var argsObj interface{}
				if tc.Arguments != nil {
					argsObj = tc.Arguments
				} else if tc.Function != nil && tc.Function.Arguments != "" {
					var parsed map[string]interface{}
					if json.Unmarshal([]byte(tc.Function.Arguments), &parsed) == nil {
						argsObj = parsed
					} else {
						argsObj = map[string]interface{}{}
					}
				}
				parts = append(parts, map[string]interface{}{
					"functionCall": map[string]interface{}{
						"name": name,
						"args": argsObj,
					},
				})
			}
			if len(parts) > 0 {
				contents = append(contents, map[string]interface{}{
					"role":  "model",
					"parts": parts,
				})
			}

		default: // "user"
			contents = append(contents, map[string]interface{}{
				"role": "user",
				"parts": []map[string]interface{}{
					{"text": m.Content},
				},
			})
		}
	}

	return contents, systemText
}

// --- Response parsing ---

func parseVertexResponse(body []byte) (*LLMResponse, error) {
	var resp struct {
		Candidates []struct {
			Content struct {
				Role  string `json:"role"`
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name string                 `json:"name"`
						Args map[string]interface{} `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("vertex: failed to parse response: %w\nraw: %s", err, string(body))
	}

	if len(resp.Candidates) == 0 {
		return nil, fmt.Errorf("vertex: empty candidates in response")
	}

	cand := resp.Candidates[0]
	result := &LLMResponse{
		FinishReason: strings.ToLower(cand.FinishReason),
		Usage: &UsageInfo{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		},
	}

	for _, part := range cand.Content.Parts {
		if part.Text != "" {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			argsJSON, _ := json.Marshal(part.FunctionCall.Args)
			result.ToolCalls = append(result.ToolCalls, ToolCall{
				ID:   fmt.Sprintf("call_%s", part.FunctionCall.Name),
				Type: "function",
				Name: part.FunctionCall.Name,
				Function: &FunctionCall{
					Name:      part.FunctionCall.Name,
					Arguments: string(argsJSON),
				},
				Arguments: part.FunctionCall.Args,
			})
		}
	}

	return result, nil
}

func parseVertexError(status int, body []byte) error {
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Code    int    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Error.Message != "" {
		switch status {
		case 401, 403:
			return fmt.Errorf("vertex auth error: %s\n  Run: gcloud auth application-default login", apiErr.Error.Message)
		case 404:
			return fmt.Errorf("vertex model not found: %s\n  Check project_id, location, and model name in config", apiErr.Error.Message)
		case 429:
			return fmt.Errorf("vertex quota exceeded: %s\n  Check your Vertex AI quotas in the Google Cloud Console", apiErr.Error.Message)
		default:
			return fmt.Errorf("vertex API error (%d): %s", status, apiErr.Error.Message)
		}
	}
	return fmt.Errorf("vertex API error (%d): %s", status, string(body))
}

// --- OAuth2 authentication ---

// tokenFromGCloud gets an access token via `gcloud auth print-access-token`.
func tokenFromGCloud(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "gcloud", "auth", "print-access-token")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gcloud auth print-access-token: %w", err)
	}
	tok := strings.TrimSpace(string(out))
	if tok == "" {
		return "", fmt.Errorf("gcloud returned empty token")
	}
	return tok, nil
}

// tokenFromServiceAccount exchanges a service account JSON file for an access token.
func tokenFromServiceAccount(ctx context.Context, saPath string) (string, time.Time, error) {
	data, err := os.ReadFile(filepath.Clean(saPath))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("read SA JSON: %w", err)
	}

	var sa struct {
		Type         string `json:"type"`
		ProjectID    string `json:"project_id"`
		ClientEmail  string `json:"client_email"`
		PrivateKeyID string `json:"private_key_id"`
		PrivateKey   string `json:"private_key"`
		TokenURI     string `json:"token_uri"`
	}
	if err := json.Unmarshal(data, &sa); err != nil {
		return "", time.Time{}, fmt.Errorf("parse SA JSON: %w", err)
	}
	if sa.Type != "service_account" {
		return "", time.Time{}, fmt.Errorf("not a service account JSON (type=%q)", sa.Type)
	}
	if sa.PrivateKey == "" || sa.ClientEmail == "" {
		return "", time.Time{}, fmt.Errorf("SA JSON missing private_key or client_email")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	// Build JWT.
	now := time.Now()
	jwt, err := buildServiceAccountJWT(sa.ClientEmail, sa.PrivateKey, sa.TokenURI, now)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build JWT: %w", err)
	}

	// Exchange JWT for access token.
	vals := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {jwt},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", sa.TokenURI, strings.NewReader(vals.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		if tokenResp.Error != "" {
			return "", time.Time{}, fmt.Errorf("token exchange: %s", tokenResp.Error)
		}
		return "", time.Time{}, fmt.Errorf("token exchange failed: %s", string(body))
	}

	expiry := now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	return tokenResp.AccessToken, expiry, nil
}

// buildServiceAccountJWT creates a signed JWT for the Google OAuth2 token endpoint.
func buildServiceAccountJWT(email, privateKeyPEM, tokenURI string, now time.Time) (string, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return "", fmt.Errorf("failed to decode PEM block from private key")
	}

	var privKey *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parse PKCS1 key: %w", err)
		}
		privKey = key
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return "", fmt.Errorf("parse PKCS8 key: %w", err)
		}
		var ok bool
		privKey, ok = key.(*rsa.PrivateKey)
		if !ok {
			return "", fmt.Errorf("private key is not RSA")
		}
	default:
		return "", fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}

	header := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
	}))
	claims := base64.RawURLEncoding.EncodeToString(mustJSON(map[string]interface{}{
		"iss":   email,
		"sub":   email,
		"aud":   tokenURI,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}))

	sigInput := header + "." + claims
	hashed := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, privKey, 5 /* crypto.SHA256 */, hashed[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return sigInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func mustJSON(v interface{}) []byte {
	b, _ := json.Marshal(v)
	return b
}

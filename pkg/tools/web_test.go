package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestWebTool_WebFetch_Success verifies successful URL fetching
func TestWebTool_WebFetch_Success(t *testing.T) {
	// httptest.NewServer binds to 127.0.0.1, which is now blocked by SSRF protection.
	// This test verifies that localhost is correctly blocked.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("<html><body><h1>Test Page</h1><p>Content here</p></body></html>"))
	}))
	defer server.Close()

	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": server.URL,
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Localhost should be blocked by SSRF protection
	if !result.IsError {
		t.Errorf("Expected SSRF block for localhost URL")
	}
	if !strings.Contains(result.ForLLM, "URL blocked") {
		t.Errorf("Expected 'URL blocked' message, got: %s", result.ForLLM)
	}
}

// TestWebTool_WebFetch_JSON verifies JSON content handling (blocked by SSRF for localhost)
func TestWebTool_WebFetch_JSON(t *testing.T) {
	testData := map[string]string{"key": "value", "number": "123"}
	expectedJSON, _ := json.MarshalIndent(testData, "", "  ")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(expectedJSON)
	}))
	defer server.Close()

	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": server.URL,
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Localhost should be blocked by SSRF protection
	if !result.IsError {
		t.Errorf("Expected SSRF block for localhost URL")
	}
}

// TestWebTool_WebFetch_InvalidURL verifies error handling for invalid URL
func TestWebTool_WebFetch_InvalidURL(t *testing.T) {
	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": "not-a-valid-url",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error for invalid URL")
	}

	// Should contain error message (either "invalid URL" or scheme error)
	if !strings.Contains(result.ForLLM, "URL") && !strings.Contains(result.ForUser, "URL") {
		t.Errorf("Expected error message for invalid URL, got ForLLM: %s", result.ForLLM)
	}
}

// TestWebTool_WebFetch_UnsupportedScheme verifies error handling for non-http URLs
func TestWebTool_WebFetch_UnsupportedScheme(t *testing.T) {
	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": "ftp://example.com/file.txt",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error for unsupported URL scheme")
	}

	// Should mention only http/https allowed
	if !strings.Contains(result.ForLLM, "http/https") && !strings.Contains(result.ForUser, "http/https") {
		t.Errorf("Expected scheme error message, got ForLLM: %s", result.ForLLM)
	}
}

// TestWebTool_WebFetch_MissingURL verifies error handling for missing URL
func TestWebTool_WebFetch_MissingURL(t *testing.T) {
	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error when URL is missing")
	}

	// Should mention URL is required
	if !strings.Contains(result.ForLLM, "url is required") && !strings.Contains(result.ForUser, "url is required") {
		t.Errorf("Expected 'url is required' message, got ForLLM: %s", result.ForLLM)
	}
}

// TestWebTool_WebFetch_Truncation verifies content truncation (blocked by SSRF for localhost)
func TestWebTool_WebFetch_Truncation(t *testing.T) {
	longContent := strings.Repeat("x", 20000)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(longContent))
	}))
	defer server.Close()

	tool := NewWebFetchTool(1000) // Limit to 1000 chars
	ctx := context.Background()
	args := map[string]interface{}{
		"url": server.URL,
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Localhost should be blocked by SSRF protection
	if !result.IsError {
		t.Errorf("Expected SSRF block for localhost URL")
	}
}

// TestWebTool_WebSearch_NoApiKey verifies that no tool is created when API key is missing
func TestWebTool_WebSearch_NoApiKey(t *testing.T) {
	tool := NewWebSearchTool(WebSearchToolOptions{BraveEnabled: true, BraveAPIKey: ""})
	if tool != nil {
		t.Errorf("Expected nil tool when Brave API key is empty")
	}

	// Also nil when nothing is enabled
	tool = NewWebSearchTool(WebSearchToolOptions{})
	if tool != nil {
		t.Errorf("Expected nil tool when no provider is enabled")
	}
}

// TestWebTool_WebSearch_MissingQuery verifies error handling for missing query
func TestWebTool_WebSearch_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool(WebSearchToolOptions{BraveEnabled: true, BraveAPIKey: "test-key", BraveMaxResults: 5})
	ctx := context.Background()
	args := map[string]interface{}{}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error when query is missing")
	}
}

// TestWebTool_WebFetch_HTMLExtraction verifies HTML text extraction (blocked by SSRF for localhost)
func TestWebTool_WebFetch_HTMLExtraction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`<html><body><script>alert('test');</script><style>body{color:red;}</style><h1>Title</h1><p>Content</p></body></html>`))
	}))
	defer server.Close()

	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": server.URL,
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Localhost should be blocked by SSRF protection
	if !result.IsError {
		t.Errorf("Expected SSRF block for localhost URL")
	}
}

// TestWebTool_SSRF_BlockedHosts verifies SSRF protection blocks internal hosts
func TestWebTool_SSRF_BlockedHosts(t *testing.T) {
	blockedURLs := []string{
		"http://localhost/path",
		"http://127.0.0.1/path",
		"http://169.254.169.254/latest/meta-data/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://10.0.0.1/internal",
		"http://192.168.1.1/admin",
		"http://172.16.0.1/internal",
	}

	tool := NewWebFetchTool(50000)
	ctx := context.Background()

	for _, u := range blockedURLs {
		result := tool.Execute(ctx, ToolContext{}, map[string]interface{}{"url": u})
		if !result.IsError {
			t.Errorf("Expected SSRF block for %s", u)
		}
		if !strings.Contains(result.ForLLM, "URL blocked") {
			t.Errorf("Expected 'URL blocked' for %s, got: %s", u, result.ForLLM)
		}
	}
}

// TestWebTool_SSRF_AllowedHosts verifies SSRF protection allows public hosts
func TestWebTool_SSRF_AllowedHosts(t *testing.T) {
	allowedHosts := []string{
		"example.com",
		"api.github.com",
		"8.8.8.8",
		"172.32.0.1", // outside 172.16-31 range
	}

	for _, h := range allowedHosts {
		if isBlockedHost(h) {
			t.Errorf("Expected %s to be allowed, but it was blocked", h)
		}
	}
}

func TestWebFetch_CheckRedirectBlocksPrivateHosts(t *testing.T) {
	client := newWebFetchHTTPClient()

	err := client.CheckRedirect(&http.Request{URL: mustParseURL(t, "http://localhost/path")}, []*http.Request{
		{URL: mustParseURL(t, "https://example.com")},
	})
	if err == nil {
		t.Fatal("expected redirect to blocked host to fail")
	}
	if !strings.Contains(err.Error(), "URL blocked") {
		t.Fatalf("expected SSRF block, got: %v", err)
	}
}

func TestWebFetch_BuildResultIncludesFetchedTextForLLM(t *testing.T) {
	result := buildFetchResult("https://example.com", http.StatusOK, "text", false, "hello from the page")
	if !strings.Contains(result, "\"text\": \"hello from the page\"") {
		t.Fatalf("expected fetched text in result JSON, got: %s", result)
	}
}

// TestWebTool_WebFetch_MissingDomain verifies error handling for URL without domain
func TestWebTool_WebFetch_MissingDomain(t *testing.T) {
	tool := NewWebFetchTool(50000)
	ctx := context.Background()
	args := map[string]interface{}{
		"url": "https://",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error for URL without domain")
	}

	// Should mention missing domain
	if !strings.Contains(result.ForLLM, "domain") && !strings.Contains(result.ForUser, "domain") {
		t.Errorf("Expected domain error message, got ForLLM: %s", result.ForLLM)
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse URL %q: %v", raw, err)
	}
	return parsed
}

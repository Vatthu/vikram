package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/v1claw/levik/pkg/events"
	"github.com/v1claw/levik/pkg/state"
	devsync "github.com/v1claw/levik/pkg/sync"
)

func newTestServer(t *testing.T) (*Server, *state.Manager) {
	tmpDir := t.TempDir()
	stateMgr := state.NewManager(tmpDir)
	router := events.NewRouter()

	// NewServer now takes a Config with RateLimit
	srv := NewServer(Config{Addr: ":0", APIKey: "testkey"}, nil, router, stateMgr, nil)
	return srv, stateMgr
}

func newTestDeviceServer() *Server {
	registry := devsync.NewRegistry(devsync.DeviceInfo{
		ID:       "self",
		Name:     "self",
		Host:     "127.0.0.1",
		Platform: "darwin",
		Version:  "test",
	})
	return NewServer(Config{Addr: ":0", APIKey: "testkey"}, nil, nil, nil, registry)
}

func TestHealthEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "ok", resp["status"])
}

func TestReadyEndpoint_NotReady(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	srv.handleReady(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestReadyEndpoint_Ready(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetChatHandler(func(ctx context.Context, msg, sess string) (string, error) {
		return "ok", nil
	})

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	srv.handleReady(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestStatusEndpoint(t *testing.T) {
	srv, stateMgr := newTestServer(t)
	stateMgr.SetUserState("u1", "telegram", "1", "alice")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp StatusResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, 1, resp.TrackedUsers)
}

func TestChatEndpoint_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetChatHandler(func(ctx context.Context, msg, sess string) (string, error) {
		return "Hello! You said: " + msg, nil
	})

	body, _ := json.Marshal(ChatRequest{Message: "Hi", SessionKey: "test"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp ChatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "Hello! You said: Hi", resp.Response)
	assert.Equal(t, "test", resp.SessionKey)
}

func TestChatEndpoint_EmptyMessage(t *testing.T) {
	srv, _ := newTestServer(t)

	body, _ := json.Marshal(ChatRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestChatEndpoint_MessageTooLong(t *testing.T) {
	srv, _ := newTestServer(t)

	body, _ := json.Marshal(ChatRequest{Message: strings.Repeat("x", maxChatMessageChars+1)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp.Error, "maximum length")
}

func TestChatEndpoint_NoHandler(t *testing.T) {
	srv, _ := newTestServer(t)

	body, _ := json.Marshal(ChatRequest{Message: "Hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestChatEndpoint_DefaultSessionKey(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetChatHandler(func(ctx context.Context, msg, sess string) (string, error) {
		return sess, nil
	})

	body, _ := json.Marshal(ChatRequest{Message: "Hi"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	var resp ChatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Equal(t, "api:default", resp.Response)
}

func TestChatEndpoint_WrongMethod(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/chat", nil)
	w := httptest.NewRecorder()
	srv.handleChat(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestWSChat_UsesNonNilContext(t *testing.T) {
	srv, _ := newTestServer(t)

	ctxErrCh := make(chan error, 1)
	srv.SetChatHandler(func(ctx context.Context, msg, sess string) (string, error) {
		if ctx == nil {
			ctxErrCh <- fmt.Errorf("nil context")
			return "", nil
		}
		if _, ok := ctx.Deadline(); !ok {
			ctxErrCh <- fmt.Errorf("missing deadline")
			return "", nil
		}
		ctxErrCh <- nil
		return "ok", nil
	})

	client := &wsClient{
		id:   "test_ws",
		send: make(chan []byte, 2),
	}

	srv.handleWSChat(client, WSMessage{
		Type: "chat",
		Data: map[string]interface{}{
			"message":     "hello",
			"session_key": "ws:test",
		},
	})

	select {
	case err := <-ctxErrCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for websocket handler invocation")
	}
}

func TestWSChat_RejectsOversizedMessage(t *testing.T) {
	srv, _ := newTestServer(t)

	handlerCalled := make(chan struct{}, 1)
	srv.SetChatHandler(func(ctx context.Context, msg, sess string) (string, error) {
		handlerCalled <- struct{}{}
		return "ok", nil
	})

	client := &wsClient{
		id:   "test_ws",
		send: make(chan []byte, 2),
	}

	srv.handleWSChat(client, WSMessage{
		Type: "chat",
		Data: map[string]interface{}{
			"message": strings.Repeat("x", maxChatMessageChars+1),
		},
	})

	select {
	case <-handlerCalled:
		t.Fatal("chat handler should not be called for oversized websocket messages")
	default:
	}

	select {
	case raw := <-client.send:
		var msg WSMessage
		require.NoError(t, json.Unmarshal(raw, &msg))
		assert.Equal(t, "error", msg.Type)
	case <-time.After(time.Second):
		t.Fatal("expected websocket error response")
	}
}

func TestRequestCapability_FailsWhenClientConnectionUnavailable(t *testing.T) {
	srv, _ := newTestServer(t)

	clientCtx, cancel := context.WithCancel(context.Background())
	cancel()

	client := &wsClient{
		id:       "ws_test",
		deviceID: "device-1",
		send:     make(chan []byte, 1),
		capReqs:  make(map[string]chan CapabilityResponse),
		ctx:      clientCtx,
		cancel:   func() {},
	}

	srv.mu.Lock()
	srv.wsClients[client.id] = client
	srv.mu.Unlock()

	_, err := srv.RequestCapability(context.Background(), "device-1", CapabilityRequest{
		RequestID:  "req-1",
		Capability: "speaker",
		Action:     "play",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection unavailable")

	client.mu.Lock()
	_, ok := client.capReqs["req-1"]
	client.mu.Unlock()
	assert.False(t, ok)
}

func TestRemoveWSClient_FailsPendingCapabilityRequests(t *testing.T) {
	srv, _ := newTestServer(t)

	clientCtx, cancel := context.WithCancel(context.Background())
	client := &wsClient{
		id:       "ws_test",
		deviceID: "device-1",
		send:     make(chan []byte, 1),
		capReqs:  make(map[string]chan CapabilityResponse),
		ctx:      clientCtx,
		cancel:   cancel,
	}

	respCh := make(chan CapabilityResponse, 1)
	client.capReqs["req-1"] = respCh

	srv.mu.Lock()
	srv.wsClients[client.id] = client
	srv.mu.Unlock()

	srv.removeWSClient(client.id)

	select {
	case resp := <-respCh:
		assert.Equal(t, "req-1", resp.RequestID)
		assert.False(t, resp.Success)
		assert.Contains(t, resp.Error, "disconnected")
	case <-time.After(time.Second):
		t.Fatal("expected pending capability request to fail on disconnect")
	}

	select {
	case <-clientCtx.Done():
	default:
		t.Fatal("expected websocket client context to be cancelled")
	}

	srv.mu.RLock()
	_, ok := srv.wsClients[client.id]
	srv.mu.RUnlock()
	assert.False(t, ok)
}

func TestHandleDevices_RejectsMissingWSRegisterToken(t *testing.T) {
	srv := newTestDeviceServer()
	srv.wsClients["ws-1"] = &wsClient{
		id:            "ws-1",
		registerToken: "token-123",
		send:          make(chan []byte, 1),
		capReqs:       make(map[string]chan CapabilityResponse),
	}

	body, _ := json.Marshal(DeviceRegisterRequest{
		ID:         "device-1",
		Name:       "Device 1",
		Host:       "device.example.com",
		Platform:   "linux",
		WSClientID: "ws-1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleDevices(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Error, "ws_register_token")
}

func TestHandleDevices_RejectsInvalidWSRegisterToken(t *testing.T) {
	srv := newTestDeviceServer()
	srv.wsClients["ws-1"] = &wsClient{
		id:            "ws-1",
		registerToken: "token-123",
		send:          make(chan []byte, 1),
		capReqs:       make(map[string]chan CapabilityResponse),
	}

	body, _ := json.Marshal(DeviceRegisterRequest{
		ID:              "device-1",
		Name:            "Device 1",
		Host:            "device.example.com",
		Platform:        "linux",
		WSClientID:      "ws-1",
		WSRegisterToken: "wrong-token",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleDevices(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp.Error, "invalid websocket registration token")
}

func TestHandleDevices_AssociatesClientWithValidWSRegisterToken(t *testing.T) {
	srv := newTestDeviceServer()
	client := &wsClient{
		id:            "ws-1",
		registerToken: "token-123",
		send:          make(chan []byte, 1),
		capReqs:       make(map[string]chan CapabilityResponse),
	}
	srv.wsClients["ws-1"] = client

	body, _ := json.Marshal(DeviceRegisterRequest{
		ID:              "device-1",
		Name:            "Device 1",
		Host:            "device.example.com",
		Platform:        "linux",
		WSClientID:      "ws-1",
		WSRegisterToken: "token-123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleDevices(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "device-1", client.deviceID)
	require.NotNil(t, srv.registry.Get("device-1"))
}

func TestHandleDevices_AllowsPrivateDeviceHost(t *testing.T) {
	srv := newTestDeviceServer()
	client := &wsClient{
		id:            "ws-1",
		registerToken: "token-123",
		send:          make(chan []byte, 1),
		capReqs:       make(map[string]chan CapabilityResponse),
	}
	srv.wsClients["ws-1"] = client

	body, _ := json.Marshal(DeviceRegisterRequest{
		ID:              "device-lan",
		Name:            "LAN Device",
		Host:            "192.168.1.50",
		Platform:        "linux",
		WSClientID:      "ws-1",
		WSRegisterToken: "token-123",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/devices", bytes.NewReader(body))
	w := httptest.NewRecorder()

	srv.handleDevices(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, srv.registry.Get("device-lan"))
}

func TestValidateDeviceHost_AllowsLANAndMDNSHosts(t *testing.T) {
	for _, host := range []string{
		"192.168.1.10",
		"10.0.0.15",
		"172.16.5.20",
		"100.101.102.103",
		"phone.local",
		"tablet.internal",
	} {
		require.NoError(t, validateDeviceHost(host), "host %s should be accepted", host)
	}
}

func TestValidateDeviceHost_RejectsLoopbackUnspecifiedAndMulticast(t *testing.T) {
	for _, host := range []string{
		"localhost",
		"127.0.0.1",
		"::1",
		"0.0.0.0",
		"::",
		"224.0.0.1",
	} {
		require.Error(t, validateDeviceHost(host), "host %s should be rejected", host)
	}
}

func TestUsersEndpoint(t *testing.T) {
	srv, stateMgr := newTestServer(t)
	stateMgr.SetUserState("telegram:alice", "telegram", "111", "alice")
	stateMgr.SetUserState("discord:bob", "discord", "222", "bob")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	w := httptest.NewRecorder()
	srv.handleUsers(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp UsersResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Len(t, resp.Users, 2)
}

func TestEventsEndpoint(t *testing.T) {
	srv, _ := newTestServer(t)

	ctx := context.Background()
	srv.router.Start(ctx)
	defer srv.router.Stop()

	body, _ := json.Marshal(EventRequest{
		Kind:    "custom",
		Payload: map[string]interface{}{"key": "value"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleEvents(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
}

func TestEventsEndpoint_MissingKind(t *testing.T) {
	srv, _ := newTestServer(t)

	body, _ := json.Marshal(EventRequest{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleEvents(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthMiddleware_NoKey(t *testing.T) {
	srv, _ := newTestServer(t)
	// No API key configured — requests should fail
	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // This should not be reached
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	assert.Contains(t, resp.Error, "invalid or missing API key")
}

func TestAuthMiddleware_ValidKey(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.apiKey = "secret123"

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_InvalidKey(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.apiKey = "secret123"

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	handler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAuthMiddleware_QueryParam verifies that API keys in URL query parameters
// are rejected (V-20 security fix: keys in query params appear in server logs).
func TestAuthMiddleware_QueryParam(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.apiKey = "secret123"

	handler := srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/?api_key=secret123", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	// Query-param auth must be rejected — use Authorization header instead.
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestNewServer_DefaultAddr(t *testing.T) {
	srv := NewServer(Config{}, nil, nil, nil, nil)
	assert.Equal(t, ":18791", srv.addr)
}

func TestNewServer_CustomAddr(t *testing.T) {
	srv := NewServer(Config{Addr: ":9090"}, nil, nil, nil, nil)
	assert.Equal(t, ":9090", srv.addr)
}

func TestStatusEndpoint_WrongMethod(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	w := httptest.NewRecorder()
	srv.handleStatus(w, req)

	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func TestServerStartStop(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.addr = ":0" // Random port

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start(ctx)
	}()

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	cancel()

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop in time")
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	// Create a server with rate limiting enabled (e.g., 1 request per second, 1 burst)
	srv := NewServer(Config{Addr: ":0", APIKey: "testkey", RateLimit: 1.0}, nil, nil, nil, nil)

	handler := srv.rateLimitMiddleware(srv.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should succeed
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req1.Header.Set("Authorization", "Bearer testkey")
	w1 := httptest.NewRecorder()
	handler(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second request immediately after should be rate limited
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.Header.Set("Authorization", "Bearer testkey")
	w2 := httptest.NewRecorder()
	handler(w2, req2)
	assert.Equal(t, http.StatusTooManyRequests, w2.Code)
	var resp ErrorResponse
	json.NewDecoder(w2.Body).Decode(&resp)
	assert.Contains(t, resp.Error, "rate limit exceeded")

	// Wait for rate limiter to refresh (1 second for 1.0/s rate)
	time.Sleep(1 * time.Second)

	// Third request after refresh should succeed
	req3 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req3.Header.Set("Authorization", "Bearer testkey")
	w3 := httptest.NewRecorder()
	handler(w3, req3)
	assert.Equal(t, http.StatusOK, w3.Code)
}

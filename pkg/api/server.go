package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/events"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/state"
	devsync "github.com/Vatthu/vikram/pkg/sync"
)

const (
	maxInboundBodyBytes = 1 << 20
	maxChatMessageChars = 128 * 1024
	maxSessionKeyChars  = 256
)

// Server provides HTTP and WebSocket API endpoints for Vikram.
type Server struct {
	mu          sync.RWMutex
	addr        string
	apiKey      string
	msgBus      *bus.MessageBus
	router      *events.Router
	stateMgr    *state.Manager
	registry    *devsync.Registry
	httpServer  *http.Server
	wsClients   map[string]*wsClient
	chatHandler ChatHandler
	// Per-IP rate limiting — prevents a single client from monopolising the API.
	ipLimiters sync.Map   // map[string]*rate.Limiter
	rateLimit  rate.Limit // configured req/s; 0 = disabled
	rateBurst  int        // token bucket burst size
}

// ChatHandler processes chat messages from the API and returns responses.
type ChatHandler func(ctx context.Context, message, sessionKey string) (string, error)

// Config holds API server configuration.
type Config struct {
	// Addr is the listen address (default ":18791").
	Addr string `json:"addr"`
	// APIKey is the authentication key for API access. Empty disables auth.
	APIKey string `json:"api_key"`
	// RateLimit is the maximum requests per second allowed for the API. 0 disables rate limiting.
	RateLimit float64 `json:"rate_limit"`
}

// NewServer creates a new API server.
func NewServer(cfg Config, msgBus *bus.MessageBus, router *events.Router, stateMgr *state.Manager, registry *devsync.Registry) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":18791"
	}
	srv := &Server{
		addr:      cfg.Addr,
		apiKey:    cfg.APIKey,
		msgBus:    msgBus,
		router:    router,
		stateMgr:  stateMgr,
		registry:  registry,
		wsClients: make(map[string]*wsClient),
	}

	if cfg.RateLimit > 0 {
		srv.rateLimit = rate.Limit(cfg.RateLimit)
		srv.rateBurst = int(cfg.RateLimit)
		if srv.rateBurst < 1 {
			srv.rateBurst = 1
		}
		logger.InfoC("api", fmt.Sprintf("API rate limit enabled: %.2f req/s per IP", cfg.RateLimit))
	} else {
		logger.InfoC("api", "API rate limiting disabled")
	}

	return srv
}

// SetChatHandler sets the function that processes chat messages.
func (s *Server) SetChatHandler(handler ChatHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chatHandler = handler
}

// Start begins serving HTTP requests.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/v1/chat", s.rateLimitMiddleware(s.authMiddleware(s.handleChat)))
	mux.HandleFunc("/api/v1/status", s.rateLimitMiddleware(s.authMiddleware(s.handleStatus)))
	mux.HandleFunc("/api/v1/users", s.rateLimitMiddleware(s.authMiddleware(s.handleUsers)))
	mux.HandleFunc("/api/v1/events", s.rateLimitMiddleware(s.authMiddleware(s.handleEvents)))
	mux.HandleFunc("/api/v1/ws", s.rateLimitMiddleware(s.authMiddleware(s.handleWebSocket)))

	// Device registration routes
	mux.HandleFunc("/api/v1/devices", s.rateLimitMiddleware(s.authMiddleware(s.handleDevices)))
	mux.HandleFunc("/api/v1/devices/", s.rateLimitMiddleware(s.authMiddleware(s.handleDeviceByID)))

	// Health endpoints (no auth)
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ready", s.handleReady)

	s.httpServer = &http.Server{
		Addr:         s.addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 120 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Subscribe to events for WebSocket broadcast.
	if s.router != nil {
		s.router.Subscribe("api_ws_broadcast", func(ctx context.Context, event events.Event) error {
			s.broadcastEvent(event)
			return nil
		})
	}

	logger.InfoC("api", fmt.Sprintf("API server starting on %s", s.addr))

	// Warn loudly when authentication is disabled so operators don't miss it.
	if s.apiKey == "" {
		logger.WarnC("api", "SECURITY WARNING: API authentication is disabled — set api.api_key in config before exposing this server")
	}

	// Prevent ipLimiters from growing without bound: sweep stale entries hourly.
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.ipLimiters.Range(func(k, _ interface{}) bool {
					s.ipLimiters.Delete(k)
					return true
				})
			}
		}
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(shutdownCtx)
	}()

	err := s.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.httpServer.Shutdown(ctx)
	}
	s.closeAllWS()
	logger.InfoC("api", "API server stopped")
}

// --- Middleware ---

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiKey == "" {
			// API key not configured — auth disabled (INSECURE: configure api_key in config)
			next(w, r)
			return
		}

		// Only accept the Authorization header — never URL query parameters.
		// Query-param keys appear in server access logs (plain text) and proxy logs.
		auth := r.Header.Get("Authorization")

		token := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(token), []byte(s.apiKey)) != 1 {
			writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid or missing API key"})
			return
		}

		next(w, r)
	}
}

// limiterForIP returns the per-IP token-bucket rate limiter, creating one if needed.
func (s *Server) limiterForIP(ip string) *rate.Limiter {
	if v, ok := s.ipLimiters.Load(ip); ok {
		return v.(*rate.Limiter)
	}
	l := rate.NewLimiter(s.rateLimit, s.rateBurst)
	actual, _ := s.ipLimiters.LoadOrStore(ip, l)
	return actual.(*rate.Limiter)
}

func (s *Server) rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.rateLimit == 0 {
			next(w, r)
			return
		}

		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = r.RemoteAddr
		}
		if !s.limiterForIP(ip).Allow() {
			writeJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "rate limit exceeded"})
			return
		}

		next(w, r)
	}
}

// --- Handlers ---

func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	var req ChatRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxInboundBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.SessionKey == "" {
		req.SessionKey = "api:default"
	}

	if err := validateChatInput(req.Message, req.SessionKey); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	s.mu.RLock()
	handler := s.chatHandler
	s.mu.RUnlock()

	if handler == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "chat handler not configured"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	response, err := handler(ctx, req.Message, req.SessionKey)
	if err != nil {
		logger.WarnCF("api", "Chat handler error", map[string]interface{}{"error": err.Error()})
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "internal error processing request"})
		return
	}

	writeJSON(w, http.StatusOK, ChatResponse{
		Response:   response,
		SessionKey: req.SessionKey,
		Timestamp:  time.Now(),
	})
}

func validateChatInput(message, sessionKey string) error {
	if message == "" {
		return fmt.Errorf("message is required")
	}
	if len(message) > maxChatMessageChars {
		return fmt.Errorf("message exceeds maximum length of %d characters", maxChatMessageChars)
	}
	if len(sessionKey) > maxSessionKeyChars {
		return fmt.Errorf("session_key exceeds maximum length of %d characters", maxSessionKeyChars)
	}
	return nil
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	status := StatusResponse{
		Status:    "running",
		Timestamp: time.Now(),
	}

	if s.router != nil {
		status.EventSources = s.router.SourceCount()
		status.EventSubscriptions = s.router.SubscriptionCount()
		status.EventRouterRunning = s.router.IsRunning()
	}

	if s.stateMgr != nil {
		status.TrackedUsers = s.stateMgr.UserCount()
	}

	s.mu.RLock()
	status.WebSocketClients = len(s.wsClients)
	s.mu.RUnlock()

	if s.registry != nil {
		status.RegisteredDevices = s.registry.Count()
	}

	writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	if s.stateMgr == nil {
		writeJSON(w, http.StatusOK, UsersResponse{Users: map[string]*state.UserState{}})
		return
	}

	users := s.stateMgr.GetAllUsers()
	if users == nil {
		users = map[string]*state.UserState{}
	}

	writeJSON(w, http.StatusOK, UsersResponse{Users: users})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	var req EventRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB limit
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
		return
	}

	if req.Kind == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "kind is required"})
		return
	}

	if s.router == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "event router not configured"})
		return
	}

	event := events.NewEvent(req.Kind, "api", req.Priority).
		WithChannel(req.Channel, req.ChatID)
	for k, v := range req.Payload {
		event = event.WithPayload(k, v)
	}

	s.router.Emit(event)

	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":   "accepted",
		"event_id": event.ID,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ready := true
	s.mu.RLock()
	if s.chatHandler == nil {
		ready = false
	}
	s.mu.RUnlock()

	if ready {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	} else {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
	}
}

// --- Device Registration Handlers ---

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "device registry not configured"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		// GET /api/v1/devices — list all devices
		capFilter := r.URL.Query().Get("capability")
		var devices []devsync.DeviceInfo
		if capFilter != "" {
			devices = s.registry.WithCapability(devsync.DeviceCapability(capFilter))
		} else {
			devices = s.registry.All()
		}
		writeJSON(w, http.StatusOK, DevicesResponse{Devices: devices})

	case http.MethodPost:
		// POST /api/v1/devices — register a device
		var req DeviceRegisterRequest
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}
		if req.ID == "" || req.Name == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "id and name are required"})
			return
		}
		if err := validateDeviceHost(req.Host); err != nil {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: err.Error()})
			return
		}
		if req.WSRegisterToken != "" && req.WSClientID == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "ws_client_id is required when ws_register_token is set"})
			return
		}
		if req.WSClientID != "" && req.WSRegisterToken == "" {
			writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "ws_register_token is required when ws_client_id is set"})
			return
		}

		// Validate websocket binding before mutating the registry so a failed
		// association cannot partially register a spoofed or orphaned device.
		var boundClient *wsClient
		if req.WSClientID != "" {
			s.mu.Lock()
			client, ok := s.wsClients[req.WSClientID]
			if !ok {
				s.mu.Unlock()
				writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "ws_client_id is not connected"})
				return
			}
			if subtle.ConstantTimeCompare([]byte(req.WSRegisterToken), []byte(client.registerToken)) != 1 {
				s.mu.Unlock()
				writeJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid websocket registration token"})
				return
			}
			if client.deviceID != "" && client.deviceID != req.ID {
				s.mu.Unlock()
				writeJSON(w, http.StatusConflict, ErrorResponse{Error: "ws client is already bound to another device"})
				return
			}
			boundClient = client
			s.mu.Unlock()
		}

		device := devsync.DeviceInfo{
			ID:           req.ID,
			Name:         req.Name,
			Host:         req.Host,
			Port:         req.Port,
			Platform:     req.Platform,
			Capabilities: req.Capabilities,
			Location:     req.Location,
			Version:      req.Version,
		}
		s.registry.Register(device)

		if boundClient != nil {
			s.mu.Lock()
			if client, ok := s.wsClients[req.WSClientID]; ok && client == boundClient {
				client.deviceID = req.ID
			}
			s.mu.Unlock()
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "registered",
			"device":  device.ID,
			"devices": s.registry.Count(),
		})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
	}
}

func (s *Server) handleDeviceByID(w http.ResponseWriter, r *http.Request) {
	if s.registry == nil {
		writeJSON(w, http.StatusServiceUnavailable, ErrorResponse{Error: "device registry not configured"})
		return
	}

	// Extract device ID from path: /api/v1/devices/{id}
	deviceID := strings.TrimPrefix(r.URL.Path, "/api/v1/devices/")
	if deviceID == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "device ID is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		device := s.registry.Get(deviceID)
		if device == nil {
			writeJSON(w, http.StatusNotFound, ErrorResponse{Error: "device not found"})
			return
		}
		writeJSON(w, http.StatusOK, device)

	case http.MethodDelete:
		s.registry.Unregister(deviceID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "unregistered", "device": deviceID})

	default:
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
	}
}

// Registry returns the device registry (used by client mode).
func (s *Server) Registry() *devsync.Registry {
	return s.registry
}

// --- Helpers ---

// validateDeviceHost performs lightweight sanity checks for device metadata.
// Device registration is local-network-first, so private/LAN addresses and
// mDNS hostnames must be accepted. Any future outbound connection logic must
// apply its own SSRF protections at the point of use rather than here.
func validateDeviceHost(host string) error {
	if host == "" {
		return nil // empty host is fine — device will not be contacted
	}

	// Strip port if present (e.g. "192.168.1.1:8080" → "192.168.1.1")
	h := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}

	ip := net.ParseIP(h)
	if ip == nil {
		lower := strings.ToLower(h)
		if lower == "localhost" {
			return fmt.Errorf("device host %q cannot be localhost", host)
		}
		return nil
	}

	switch {
	case ip.IsLoopback():
		return fmt.Errorf("device host %q cannot be a loopback address", host)
	case ip.IsUnspecified():
		return fmt.Errorf("device host %q cannot be an unspecified address", host)
	case ip.IsMulticast():
		return fmt.Errorf("device host %q cannot be a multicast address", host)
	}

	return nil
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.WarnCF("api", "Failed to encode JSON response", map[string]interface{}{"error": err.Error()})
	}
}

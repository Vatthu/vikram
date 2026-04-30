package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/v1claw/levik/pkg/events"
	"github.com/v1claw/levik/pkg/logger"
	devsync "github.com/v1claw/levik/pkg/sync"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin == "" {
			return true // Non-browser clients usually don't send Origin.
		}
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

const (
	wsWriteWait    = 10 * time.Second
	wsPongWait     = 90 * time.Second
	wsPingInterval = 45 * time.Second
)

type wsClient struct {
	id            string
	deviceID      string // Associated device ID for capability routing
	registerToken string
	conn          *websocket.Conn
	send          chan []byte
	mu            sync.Mutex
	capReqs       map[string]chan CapabilityResponse // Pending capability requests
	ctx           context.Context
	cancel        context.CancelFunc
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.ErrorC("api", fmt.Sprintf("WebSocket upgrade failed: %v", err))
		return
	}
	conn.SetReadLimit(maxInboundBodyBytes)

	clientID := fmt.Sprintf("ws_%d", time.Now().UnixNano())
	registerToken, err := newWSRegistrationToken()
	if err != nil {
		logger.ErrorC("api", fmt.Sprintf("WebSocket registration token generation failed: %v", err))
		_ = conn.Close()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	client := &wsClient{
		id:            clientID,
		registerToken: registerToken,
		conn:          conn,
		send:          make(chan []byte, 256), // Expanded buffer to prevent streaming drops
		capReqs:       make(map[string]chan CapabilityResponse),
		ctx:           ctx,
		cancel:        cancel,
	}

	s.mu.Lock()
	s.wsClients[clientID] = client
	s.mu.Unlock()

	logger.InfoC("api", fmt.Sprintf("WebSocket client connected: %s", clientID))

	// Send welcome message.
	welcome := WSMessage{
		Type: "connected",
		Data: map[string]string{
			"client_id":          clientID,
			"registration_token": registerToken,
		},
		Timestamp: time.Now(),
	}
	if data, err := json.Marshal(welcome); err == nil {
		if !s.enqueueClientMessage(client, data) {
			s.removeWSClient(clientID)
			return
		}
	}

	go s.wsWritePump(client)
	go s.wsReadPump(client)
}

func (s *Server) wsWritePump(client *wsClient) {
	pingTicker := time.NewTicker(wsPingInterval)
	defer func() {
		pingTicker.Stop()
		s.removeWSClient(client.id)
	}()

	for {
		select {
		case <-client.ctx.Done():
			return
		case msg, ok := <-client.send:
			if !ok {
				return
			}
			if err := s.writeWSMessage(client, websocket.TextMessage, msg); err != nil {
				logger.DebugC("api", fmt.Sprintf("WebSocket write error: %v", err))
				return
			}
		case <-pingTicker.C:
			if err := s.writeWSMessage(client, websocket.PingMessage, nil); err != nil {
				logger.DebugC("api", fmt.Sprintf("WebSocket ping error: %v", err))
				return
			}
		}
	}
}

func (s *Server) wsReadPump(client *wsClient) {
	defer func() {
		s.removeWSClient(client.id)
	}()

	client.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	client.conn.SetPongHandler(func(string) error {
		client.conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	for {
		_, message, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				logger.DebugC("api", fmt.Sprintf("WebSocket read error: %v", err))
			}
			return
		}

		// Parse incoming message.
		var wsMsg WSMessage
		if err := json.Unmarshal(message, &wsMsg); err != nil {
			continue
		}

		switch wsMsg.Type {
		case "chat":
			s.handleWSChat(client, wsMsg)
		case "ping":
			s.sendToClient(client, WSMessage{Type: "pong", Timestamp: time.Now()})
		case "capability_response":
			s.handleWSCapabilityResponse(client, wsMsg)
		}
	}
}

func (s *Server) handleWSChat(client *wsClient, msg WSMessage) {
	s.mu.RLock()
	handler := s.chatHandler
	s.mu.RUnlock()

	if handler == nil {
		s.sendToClient(client, WSMessage{
			Type:      "error",
			Data:      "chat handler not configured",
			Timestamp: time.Now(),
		})
		return
	}

	// Extract message text from data.
	text, _ := extractString(msg.Data, "message")
	sessionKey, _ := extractString(msg.Data, "session_key")
	if sessionKey == "" {
		sessionKey = "ws:" + client.id
	}
	if err := validateChatInput(text, sessionKey); err != nil {
		s.sendToClient(client, WSMessage{
			Type:      "error",
			Data:      err.Error(),
			Timestamp: time.Now(),
		})
		return
	}

	go func() {
		parentCtx := client.ctx
		if parentCtx == nil {
			parentCtx = context.Background() // Fallback for unit tests
		}
		ctx, cancel := context.WithTimeout(parentCtx, 120*time.Second) // Bounded by connection lifecycle
		defer cancel()

		response, err := handler(ctx, text, sessionKey)
		if err != nil {
			logger.WarnCF("api", "WebSocket chat handler error", map[string]interface{}{
				"session": sessionKey,
				"error":   err.Error(),
			})
			s.sendToClient(client, WSMessage{
				Type:      "error",
				Data:      "internal error processing request",
				Timestamp: time.Now(),
			})
			return
		}

		s.sendToClient(client, WSMessage{
			Type: "chat_response",
			Data: ChatResponse{
				Response:   response,
				SessionKey: sessionKey,
				Timestamp:  time.Now(),
			},
			Timestamp: time.Now(),
		})
	}()
}

func (s *Server) sendToClient(client *wsClient, msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	s.enqueueClientMessage(client, data)
}

func (s *Server) broadcastEvent(event events.Event) {
	msg := WSMessage{
		Type:      "event",
		Data:      event,
		Timestamp: time.Now(),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, client := range s.wsClients {
		s.enqueueClientMessage(client, data)
	}
}

func (s *Server) closeAllWS() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.wsClients))
	for id := range s.wsClients {
		ids = append(ids, id)
	}
	s.mu.RUnlock()

	for _, id := range ids {
		s.removeWSClient(id)
	}
}

func extractString(data interface{}, key string) (string, bool) {
	m, ok := data.(map[string]interface{})
	if !ok {
		return "", false
	}
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// handleWSCapabilityResponse processes capability responses from client devices.
func (s *Server) handleWSCapabilityResponse(client *wsClient, msg WSMessage) {
	reqID, _ := extractString(msg.Data, "request_id")
	if reqID == "" {
		return
	}

	var resp CapabilityResponse
	data, err := json.Marshal(msg.Data)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}

	client.mu.Lock()
	ch, ok := client.capReqs[reqID]
	if ok {
		delete(client.capReqs, reqID)
	}
	client.mu.Unlock()

	if ok {
		ch <- resp
	}
}

// RequestCapability sends a capability request to a specific device and waits for the response.
func (s *Server) RequestCapability(ctx context.Context, deviceID string, req CapabilityRequest) (*CapabilityResponse, error) {
	// Find the WS client associated with this device.
	s.mu.RLock()
	var target *wsClient
	for _, c := range s.wsClients {
		if c.deviceID == deviceID {
			target = c
			break
		}
	}
	s.mu.RUnlock()

	if target == nil {
		return nil, fmt.Errorf("device %s not connected via WebSocket", deviceID)
	}

	// Create response channel.
	respCh := make(chan CapabilityResponse, 1)
	target.mu.Lock()
	target.capReqs[req.RequestID] = respCh
	target.mu.Unlock()

	// Send request to client.
	if !s.trySendToClient(target, WSMessage{
		Type:      "capability_request",
		Data:      req,
		Timestamp: time.Now(),
	}) {
		target.mu.Lock()
		delete(target.capReqs, req.RequestID)
		target.mu.Unlock()
		return nil, fmt.Errorf("device %s connection unavailable", deviceID)
	}

	// Wait for response or timeout.
	select {
	case resp := <-respCh:
		return &resp, nil
	case <-ctx.Done():
		target.mu.Lock()
		delete(target.capReqs, req.RequestID)
		target.mu.Unlock()
		return nil, ctx.Err()
	}
}

// FindDeviceForCapability returns the best connected device for a capability.
func (s *Server) FindDeviceForCapability(cap string) (string, bool) {
	if s.registry == nil {
		return "", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	devices := s.registry.WithCapability(devsync.DeviceCapability(cap))
	for _, d := range devices {
		// Check if this device is connected via WS (not just registered).
		for _, c := range s.wsClients {
			if c.deviceID == d.ID {
				return d.ID, true
			}
		}
	}
	return "", false
}

// removeWSClient also unregisters the device and marks it offline.
func (s *Server) removeWSClient(id string) {
	s.mu.Lock()
	client, ok := s.wsClients[id]
	if ok {
		delete(s.wsClients, id)
	}
	s.mu.Unlock()

	if ok {
		if client.cancel != nil {
			client.cancel()
		}
		s.failPendingCapabilityRequests(client, "device disconnected")
		if client.conn != nil {
			client.mu.Lock()
			_ = client.conn.Close()
			client.mu.Unlock()
		}
		// Mark device offline if associated.
		if client.deviceID != "" && s.registry != nil {
			s.registry.MarkOffline(client.deviceID)
			logger.InfoCF("api", "Device disconnected", map[string]interface{}{
				"device_id": client.deviceID, "ws_client": id,
			})
		}
		logger.DebugC("api", fmt.Sprintf("WebSocket client disconnected: %s", id))
	}
}

func (s *Server) writeWSMessage(client *wsClient, messageType int, payload []byte) error {
	if client.conn == nil {
		return fmt.Errorf("websocket connection not initialized")
	}

	client.mu.Lock()
	defer client.mu.Unlock()

	if err := client.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)); err != nil {
		return err
	}
	return client.conn.WriteMessage(messageType, payload)
}

func (s *Server) trySendToClient(client *wsClient, msg WSMessage) bool {
	data, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	return s.enqueueClientMessage(client, data)
}

func (s *Server) enqueueClientMessage(client *wsClient, data []byte) bool {
	if client == nil {
		return false
	}

	done := clientDone(client)
	select {
	case <-done:
		return false
	default:
	}

	select {
	case client.send <- data:
		return true
	case <-done:
		return false
	case <-time.After(50 * time.Millisecond):
		logger.WarnC("api", "Dropped websocket frame due to buffer congestion")
		return false
	}
}

func (s *Server) failPendingCapabilityRequests(client *wsClient, reason string) {
	if client == nil {
		return
	}

	type pendingRequest struct {
		id string
		ch chan CapabilityResponse
	}

	client.mu.Lock()
	pending := make([]pendingRequest, 0, len(client.capReqs))
	for reqID, ch := range client.capReqs {
		pending = append(pending, pendingRequest{id: reqID, ch: ch})
		delete(client.capReqs, reqID)
	}
	client.mu.Unlock()

	for _, pendingReq := range pending {
		select {
		case pendingReq.ch <- CapabilityResponse{
			RequestID: pendingReq.id,
			Success:   false,
			Error:     reason,
		}:
		default:
		}
	}
}

func clientDone(client *wsClient) <-chan struct{} {
	if client == nil || client.ctx == nil {
		return nil
	}
	return client.ctx.Done()
}

func newWSRegistrationToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

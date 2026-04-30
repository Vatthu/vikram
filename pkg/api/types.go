package api

import (
	"time"

	"github.com/v1claw/levik/pkg/state"
	devsync "github.com/v1claw/levik/pkg/sync"
)

// ChatRequest is the request body for POST /api/v1/chat.
type ChatRequest struct {
	Message    string `json:"message"`
	SessionKey string `json:"session_key,omitempty"`
}

// ChatResponse is the response body for POST /api/v1/chat.
type ChatResponse struct {
	Response   string    `json:"response"`
	SessionKey string    `json:"session_key"`
	Timestamp  time.Time `json:"timestamp"`
}

// StatusResponse is the response body for GET /api/v1/status.
type StatusResponse struct {
	Status             string    `json:"status"`
	Timestamp          time.Time `json:"timestamp"`
	EventSources       int       `json:"event_sources"`
	EventSubscriptions int       `json:"event_subscriptions"`
	EventRouterRunning bool      `json:"event_router_running"`
	TrackedUsers       int       `json:"tracked_users"`
	WebSocketClients   int       `json:"websocket_clients"`
	RegisteredDevices  int       `json:"registered_devices"`
}

// UsersResponse is the response body for GET /api/v1/users.
type UsersResponse struct {
	Users map[string]*state.UserState `json:"users"`
}

// EventRequest is the request body for POST /api/v1/events.
type EventRequest struct {
	Kind     string                 `json:"kind"`
	Source   string                 `json:"source,omitempty"`
	Priority int                    `json:"priority,omitempty"`
	Channel  string                 `json:"channel,omitempty"`
	ChatID   string                 `json:"chat_id,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
}

// ErrorResponse is returned on errors.
type ErrorResponse struct {
	Error string `json:"error"`
}

// WSMessage represents a WebSocket message (both inbound and outbound).
type WSMessage struct {
	Type      string      `json:"type"`
	Data      interface{} `json:"data,omitempty"`
	Timestamp time.Time   `json:"timestamp"`
}

// DeviceRegisterRequest is the request body for POST /api/v1/devices.
type DeviceRegisterRequest struct {
	ID              string                     `json:"id"`
	Name            string                     `json:"name"`
	Host            string                     `json:"host"`
	Port            int                        `json:"port,omitempty"`
	Platform        string                     `json:"platform"`
	Capabilities    []devsync.DeviceCapability `json:"capabilities,omitempty"`
	Location        string                     `json:"location,omitempty"`
	Version         string                     `json:"version,omitempty"`
	WSClientID      string                     `json:"ws_client_id,omitempty"`
	WSRegisterToken string                     `json:"ws_register_token,omitempty"`
}

// DevicesResponse is the response body for GET /api/v1/devices.
type DevicesResponse struct {
	Devices []devsync.DeviceInfo `json:"devices"`
}

// CapabilityRequest is sent via WebSocket to ask a client device to perform an action.
type CapabilityRequest struct {
	RequestID  string                   `json:"request_id"`
	Capability devsync.DeviceCapability `json:"capability"`
	Action     string                   `json:"action"`
	Params     map[string]interface{}   `json:"params,omitempty"`
}

// CapabilityResponse is sent back by a client device with the result.
type CapabilityResponse struct {
	RequestID string      `json:"request_id"`
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
}

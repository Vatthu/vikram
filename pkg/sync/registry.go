package sync

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/v1claw/levik/pkg/logger"
)

// DeviceCapability represents what a device can do.
type DeviceCapability string

const (
	CapMicrophone DeviceCapability = "microphone"
	CapSpeaker    DeviceCapability = "speaker"
	CapScreen     DeviceCapability = "screen"
	CapCamera     DeviceCapability = "camera"
	CapGPIO       DeviceCapability = "gpio"
	CapBluetooth  DeviceCapability = "bluetooth"
	CapWiFi       DeviceCapability = "wifi"
)

// DeviceInfo describes a V1 instance on the network.
type DeviceInfo struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Host         string             `json:"host"`
	Port         int                `json:"port"`
	Platform     string             `json:"platform"` // "linux", "darwin", "android", "windows"
	Capabilities []DeviceCapability `json:"capabilities"`
	Location     string             `json:"location,omitempty"` // "bedroom", "kitchen", etc.
	Version      string             `json:"version"`
	LastSeen     time.Time          `json:"last_seen"`
	Online       bool               `json:"online"`
}

// SessionState represents a transferable conversation state.
type SessionState struct {
	SessionKey string            `json:"session_key"`
	UserID     string            `json:"user_id"`
	Channel    string            `json:"channel"`
	ChatID     string            `json:"chat_id"`
	Messages   []SessionMessage  `json:"messages,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// SessionMessage is a minimal message for session transfer.
type SessionMessage struct {
	Role    string `json:"role"` // "user", "assistant"
	Content string `json:"content"`
}

// Registry tracks V1 instances across the network.
type Registry struct {
	mu      sync.RWMutex
	devices map[string]*DeviceInfo
	self    *DeviceInfo
}

// NewRegistry creates a device registry.
func NewRegistry(self DeviceInfo) *Registry {
	self.Online = true
	self.LastSeen = time.Now()
	return &Registry{
		devices: map[string]*DeviceInfo{self.ID: &self},
		self:    &self,
	}
}

// Self returns this device's info.
func (r *Registry) Self() DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return *r.self
}

// Register adds or updates a device in the registry.
func (r *Registry) Register(device DeviceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device.LastSeen = time.Now()
	device.Online = true
	r.devices[device.ID] = &device
	logger.InfoCF("sync", "Device registered", map[string]interface{}{
		"id": device.ID, "name": device.Name, "host": device.Host,
	})
}

// Unregister removes a device.
func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.devices, id)
}

// Get returns a device by ID.
func (r *Registry) Get(id string) *DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if d, ok := r.devices[id]; ok {
		cp := *d
		return &cp
	}
	return nil
}

// Online returns all online devices.
func (r *Registry) Online() []DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []DeviceInfo
	for _, d := range r.devices {
		if d.Online {
			result = append(result, *d)
		}
	}
	return result
}

// All returns all known devices.
func (r *Registry) All() []DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []DeviceInfo
	for _, d := range r.devices {
		result = append(result, *d)
	}
	return result
}

// WithCapability returns devices that have a specific capability.
func (r *Registry) WithCapability(cap DeviceCapability) []DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []DeviceInfo
	for _, d := range r.devices {
		for _, c := range d.Capabilities {
			if c == cap {
				result = append(result, *d)
				break
			}
		}
	}
	return result
}

// ByLocation returns devices at a specific location.
func (r *Registry) ByLocation(location string) []DeviceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []DeviceInfo
	for _, d := range r.devices {
		if d.Location == location {
			result = append(result, *d)
		}
	}
	return result
}

// Count returns total device count.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.devices)
}

// MarkOffline marks a device as offline.
func (r *Registry) MarkOffline(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.devices[id]; ok {
		d.Online = false
	}
}

// PruneStale marks devices as offline if not seen within the timeout.
func (r *Registry) PruneStale(timeout time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := time.Now().Add(-timeout)
	pruned := 0
	for _, d := range r.devices {
		if d.Online && d.ID != r.self.ID && d.LastSeen.Before(cutoff) {
			d.Online = false
			pruned++
		}
	}
	return pruned
}

// Handoff represents a session transfer between devices.
type Handoff struct {
	mu       sync.Mutex
	registry *Registry
}

// NewHandoff creates a session handoff manager.
func NewHandoff(registry *Registry) *Handoff {
	return &Handoff{registry: registry}
}

// Transfer prepares a session for transfer to another device.
func (h *Handoff) Transfer(ctx context.Context, session SessionState, targetDeviceID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	target := h.registry.Get(targetDeviceID)
	if target == nil {
		return fmt.Errorf("device %s not found", targetDeviceID)
	}
	if !target.Online {
		return fmt.Errorf("device %s is offline", targetDeviceID)
	}

	session.UpdatedAt = time.Now()

	logger.InfoCF("sync", "Session handoff initiated", map[string]interface{}{
		"session":  session.SessionKey,
		"from":     h.registry.Self().ID,
		"to":       targetDeviceID,
		"messages": len(session.Messages),
	})

	// In a full implementation, this would POST the session to the target device's API.
	// For now, we validate the handoff is possible.
	return nil
}

// BestDeviceFor finds the best device for a given capability.
func (h *Handoff) BestDeviceFor(cap DeviceCapability) *DeviceInfo {
	devices := h.registry.WithCapability(cap)
	// Prefer self if it has the capability.
	self := h.registry.Self()
	for _, d := range devices {
		if d.ID == self.ID {
			return &d
		}
	}
	// Otherwise return first online device with capability.
	for _, d := range devices {
		if d.Online {
			return &d
		}
	}
	return nil
}

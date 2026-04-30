package state

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// State represents the persistent state for a workspace.
// It includes information about the last active channel/chat.
type State struct {
	// LastChannel is the last channel used for communication
	LastChannel string `json:"last_channel,omitempty"`

	// LastChatID is the last chat ID used for communication
	LastChatID string `json:"last_chat_id,omitempty"`

	// Timestamp is the last time this state was updated
	Timestamp time.Time `json:"timestamp"`

	// Users tracks per-user state for multi-user support.
	// Keyed by user identifier (e.g., "telegram:123456").
	Users map[string]*UserState `json:"users,omitempty"`
}

// UserState tracks the state for an individual user across channels.
type UserState struct {
	// Channel is the user's last active channel platform (e.g., "telegram").
	Channel string `json:"channel"`
	// ChatID is the user's last active chat ID on that channel.
	ChatID string `json:"chat_id"`
	// SenderID is the platform-specific user identifier.
	SenderID string `json:"sender_id,omitempty"`
	// LastActive is when this user was last seen.
	LastActive time.Time `json:"last_active"`
}

// Manager manages persistent state with atomic saves.
type Manager struct {
	workspace string
	state     *State
	mu        sync.RWMutex
	ioMu      sync.Mutex // Separates disk I/O lock from state lock
	stateFile string
}

// NewManager creates a new state manager for the given workspace.
func NewManager(workspace string) *Manager {
	stateDir := filepath.Join(workspace, "state")
	stateFile := filepath.Join(stateDir, "state.json")
	oldStateFile := filepath.Join(workspace, "state.json")

	// Create state directory if it doesn't exist
	os.MkdirAll(stateDir, 0700)

	sm := &Manager{
		workspace: workspace,
		stateFile: stateFile,
		state:     &State{},
	}

	// Try to load from new location first
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		// New file doesn't exist, try migrating from old location
		if data, err := os.ReadFile(oldStateFile); err == nil {
			if err := json.Unmarshal(data, sm.state); err == nil {
				// Migrate to new location
				sm.saveAtomic()
				log.Printf("[INFO] state: migrated state from %s to %s", oldStateFile, stateFile)
			}
		}
	} else {
		// Load from new location
		sm.load()
	}

	return sm
}

// SetLastChannel atomically updates the last channel and saves the state.
// This method uses a temp file + rename pattern for atomic writes,
// ensuring that the state file is never corrupted even if the process crashes.
func (sm *Manager) SetLastChannel(channel string) error {
	sm.mu.Lock()
	// Update state
	sm.state.LastChannel = channel
	sm.state.Timestamp = time.Now()

	data, err := json.MarshalIndent(sm.state, "", "  ")

	if err != nil {
		sm.mu.Unlock()
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sm.ioMu.Lock()
	defer sm.ioMu.Unlock()
	sm.mu.Unlock() // Lock released AFTER securing ioMu lock to preserve update order

	return sm.writeBytesAtomic(data)
}

// SetLastChatID atomically updates the last chat ID and saves the state.
func (sm *Manager) SetLastChatID(chatID string) error {
	sm.mu.Lock()
	// Update state
	sm.state.LastChatID = chatID
	sm.state.Timestamp = time.Now()

	data, err := json.MarshalIndent(sm.state, "", "  ")

	if err != nil {
		sm.mu.Unlock()
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sm.ioMu.Lock()
	defer sm.ioMu.Unlock()
	sm.mu.Unlock()
	return sm.writeBytesAtomic(data)
}

// GetLastChannel returns the last channel from the state.
func (sm *Manager) GetLastChannel() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChannel
}

// GetLastChatID returns the last chat ID from the state.
func (sm *Manager) GetLastChatID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChatID
}

// GetTimestamp returns the timestamp of the last state update.
func (sm *Manager) GetTimestamp() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.Timestamp
}

// --- Multi-User Methods ---

// SetUserState records the last active channel/chat for a specific user.
// The userKey should be a stable user identifier (e.g., "telegram:123456").
func (sm *Manager) SetUserState(userKey, channel, chatID, senderID string) error {
	sm.mu.Lock()
	if sm.state.Users == nil {
		sm.state.Users = make(map[string]*UserState)
	}

	sm.state.Users[userKey] = &UserState{
		Channel:    channel,
		ChatID:     chatID,
		SenderID:   senderID,
		LastActive: time.Now(),
	}

	// Also update the global last channel for backward compatibility.
	sm.state.LastChannel = channel + ":" + chatID
	sm.state.LastChatID = chatID
	sm.state.Timestamp = time.Now()

	data, err := json.MarshalIndent(sm.state, "", "  ")

	if err != nil {
		sm.mu.Unlock()
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sm.ioMu.Lock()
	defer sm.ioMu.Unlock()
	sm.mu.Unlock()
	return sm.writeBytesAtomic(data)
}

// GetUserState returns the state for a specific user, or nil if not found.
func (sm *Manager) GetUserState(userKey string) *UserState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.state.Users == nil {
		return nil
	}
	return sm.state.Users[userKey]
}

// GetAllUsers returns a copy of all tracked user states.
func (sm *Manager) GetAllUsers() map[string]*UserState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.state.Users == nil {
		return nil
	}

	result := make(map[string]*UserState, len(sm.state.Users))
	for k, v := range sm.state.Users {
		copied := *v
		result[k] = &copied
	}
	return result
}

// GetActiveUsers returns users active within the given duration.
func (sm *Manager) GetActiveUsers(within time.Duration) map[string]*UserState {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.state.Users == nil {
		return nil
	}

	cutoff := time.Now().Add(-within)
	result := make(map[string]*UserState)
	for k, v := range sm.state.Users {
		if v.LastActive.After(cutoff) {
			copied := *v
			result[k] = &copied
		}
	}
	return result
}

// RemoveUser removes a user from the tracked state.
func (sm *Manager) RemoveUser(userKey string) error {
	sm.mu.Lock()
	if sm.state.Users == nil {
		sm.mu.Unlock()
		return nil
	}

	delete(sm.state.Users, userKey)
	sm.state.Timestamp = time.Now()

	data, err := json.MarshalIndent(sm.state, "", "  ")

	if err != nil {
		sm.mu.Unlock()
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sm.ioMu.Lock()
	defer sm.ioMu.Unlock()
	sm.mu.Unlock()
	return sm.writeBytesAtomic(data)
}

// UserCount returns the number of tracked users.
func (sm *Manager) UserCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.state.Users == nil {
		return 0
	}
	return len(sm.state.Users)
}

// saveAtomic performs an atomic save using temp file + rename.
// Must be called with the lock held.
func (sm *Manager) saveAtomic() error {
	data, err := json.MarshalIndent(sm.state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	sm.ioMu.Lock()
	defer sm.ioMu.Unlock()
	return sm.writeBytesAtomic(data)
}

func (sm *Manager) writeBytesAtomic(data []byte) error {
	// Create temp file in the same directory as the target
	tempFile := sm.stateFile + ".tmp"

	// Write to temp file
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Atomic rename from temp to target
	if err := os.Rename(tempFile, sm.stateFile); err != nil {
		// Cleanup temp file if rename fails
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// load loads the state from disk.
func (sm *Manager) load() error {
	data, err := os.ReadFile(sm.stateFile)
	if err != nil {
		// File doesn't exist yet, that's OK
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read state file: %w", err)
	}

	if err := json.Unmarshal(data, sm.state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return nil
}

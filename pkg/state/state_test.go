package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAtomicSave(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Test SetLastChannel
	err = sm.SetLastChannel("test-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Verify the channel was saved
	lastChannel := sm.GetLastChannel()
	if lastChannel != "test-channel" {
		t.Errorf("Expected channel 'test-channel', got '%s'", lastChannel)
	}

	// Verify timestamp was updated
	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	// Verify state file exists
	stateFile := filepath.Join(tmpDir, "state", "state.json")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Error("Expected state file to exist")
	}

	// Create a new manager to verify persistence
	sm2 := NewManager(tmpDir)
	if sm2.GetLastChannel() != "test-channel" {
		t.Errorf("Expected persistent channel 'test-channel', got '%s'", sm2.GetLastChannel())
	}
}

func TestSetLastChatID(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Test SetLastChatID
	err = sm.SetLastChatID("test-chat-id")
	if err != nil {
		t.Fatalf("SetLastChatID failed: %v", err)
	}

	// Verify the chat ID was saved
	lastChatID := sm.GetLastChatID()
	if lastChatID != "test-chat-id" {
		t.Errorf("Expected chat ID 'test-chat-id', got '%s'", lastChatID)
	}

	// Verify timestamp was updated
	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	// Create a new manager to verify persistence
	sm2 := NewManager(tmpDir)
	if sm2.GetLastChatID() != "test-chat-id" {
		t.Errorf("Expected persistent chat ID 'test-chat-id', got '%s'", sm2.GetLastChatID())
	}
}

func TestAtomicity_NoCorruptionOnInterrupt(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Write initial state
	err = sm.SetLastChannel("initial-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Simulate a crash scenario by manually creating a corrupted temp file
	tempFile := filepath.Join(tmpDir, "state", "state.json.tmp")
	err = os.WriteFile(tempFile, []byte("corrupted data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Verify that the original state is still intact
	lastChannel := sm.GetLastChannel()
	if lastChannel != "initial-channel" {
		t.Errorf("Expected channel 'initial-channel' after corrupted temp file, got '%s'", lastChannel)
	}

	// Clean up the temp file manually
	os.Remove(tempFile)

	// Now do a proper save
	err = sm.SetLastChannel("new-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Verify the new state was saved
	if sm.GetLastChannel() != "new-channel" {
		t.Errorf("Expected channel 'new-channel', got '%s'", sm.GetLastChannel())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Test concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			channel := fmt.Sprintf("channel-%d", idx)
			sm.SetLastChannel(channel)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify the final state is consistent
	lastChannel := sm.GetLastChannel()
	if lastChannel == "" {
		t.Error("Expected non-empty channel after concurrent writes")
	}

	// Verify state file is valid JSON
	stateFile := filepath.Join(tmpDir, "state", "state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Errorf("State file contains invalid JSON: %v", err)
	}
}

func TestNewManager_ExistingState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create initial state
	sm1 := NewManager(tmpDir)
	sm1.SetLastChannel("existing-channel")
	sm1.SetLastChatID("existing-chat-id")

	// Create new manager with same workspace
	sm2 := NewManager(tmpDir)

	// Verify state was loaded
	if sm2.GetLastChannel() != "existing-channel" {
		t.Errorf("Expected channel 'existing-channel', got '%s'", sm2.GetLastChannel())
	}

	if sm2.GetLastChatID() != "existing-chat-id" {
		t.Errorf("Expected chat ID 'existing-chat-id', got '%s'", sm2.GetLastChatID())
	}
}

func TestNewManager_EmptyWorkspace(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Verify default state
	if sm.GetLastChannel() != "" {
		t.Errorf("Expected empty channel, got '%s'", sm.GetLastChannel())
	}

	if sm.GetLastChatID() != "" {
		t.Errorf("Expected empty chat ID, got '%s'", sm.GetLastChatID())
	}

	if !sm.GetTimestamp().IsZero() {
		t.Error("Expected zero timestamp for new state")
	}
}

func TestSetUserState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Set user state
	err = sm.SetUserState("telegram:123", "telegram", "123", "user_alice")
	if err != nil {
		t.Fatalf("SetUserState failed: %v", err)
	}

	// Verify user state
	us := sm.GetUserState("telegram:123")
	if us == nil {
		t.Fatal("Expected user state, got nil")
	}
	if us.Channel != "telegram" {
		t.Errorf("Expected channel 'telegram', got '%s'", us.Channel)
	}
	if us.ChatID != "123" {
		t.Errorf("Expected chatID '123', got '%s'", us.ChatID)
	}
	if us.SenderID != "user_alice" {
		t.Errorf("Expected senderID 'user_alice', got '%s'", us.SenderID)
	}
	if us.LastActive.IsZero() {
		t.Error("Expected non-zero LastActive")
	}

	// Verify backward compat: global last channel also updated
	if sm.GetLastChannel() != "telegram:123" {
		t.Errorf("Expected global last channel 'telegram:123', got '%s'", sm.GetLastChannel())
	}
}

func TestMultipleUsers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	// Set multiple users
	sm.SetUserState("telegram:alice", "telegram", "111", "alice")
	sm.SetUserState("discord:bob", "discord", "222", "bob")
	sm.SetUserState("slack:charlie", "slack", "333", "charlie")

	if sm.UserCount() != 3 {
		t.Errorf("Expected 3 users, got %d", sm.UserCount())
	}

	// Verify each user
	alice := sm.GetUserState("telegram:alice")
	if alice == nil || alice.Channel != "telegram" {
		t.Error("Alice state incorrect")
	}

	bob := sm.GetUserState("discord:bob")
	if bob == nil || bob.Channel != "discord" {
		t.Error("Bob state incorrect")
	}

	// Non-existent user
	if sm.GetUserState("unknown:user") != nil {
		t.Error("Expected nil for unknown user")
	}
}

func TestGetAllUsers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	sm.SetUserState("u1", "telegram", "1", "s1")
	sm.SetUserState("u2", "discord", "2", "s2")

	all := sm.GetAllUsers()
	if len(all) != 2 {
		t.Errorf("Expected 2 users, got %d", len(all))
	}

	// Verify it's a copy (modifying returned map shouldn't affect state)
	all["u1"].Channel = "modified"
	original := sm.GetUserState("u1")
	if original.Channel != "telegram" {
		t.Error("GetAllUsers should return copies, not references")
	}
}

func TestGetActiveUsers(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	sm.SetUserState("active", "telegram", "1", "s1")

	// All users should be active within 1 hour
	active := sm.GetActiveUsers(time.Hour)
	if len(active) != 1 {
		t.Errorf("Expected 1 active user, got %d", len(active))
	}
}

func TestRemoveUser(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	sm := NewManager(tmpDir)

	sm.SetUserState("u1", "telegram", "1", "s1")
	sm.SetUserState("u2", "discord", "2", "s2")

	err = sm.RemoveUser("u1")
	if err != nil {
		t.Fatalf("RemoveUser failed: %v", err)
	}

	if sm.UserCount() != 1 {
		t.Errorf("Expected 1 user after removal, got %d", sm.UserCount())
	}

	if sm.GetUserState("u1") != nil {
		t.Error("Expected u1 to be removed")
	}
}

func TestUserStatePersistence(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "state-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create state with users
	sm1 := NewManager(tmpDir)
	sm1.SetUserState("telegram:alice", "telegram", "111", "alice")
	sm1.SetUserState("discord:bob", "discord", "222", "bob")

	// Load in new manager — verify persistence
	sm2 := NewManager(tmpDir)
	if sm2.UserCount() != 2 {
		t.Errorf("Expected 2 users after reload, got %d", sm2.UserCount())
	}

	alice := sm2.GetUserState("telegram:alice")
	if alice == nil || alice.SenderID != "alice" {
		t.Error("Alice state not persisted correctly")
	}
}

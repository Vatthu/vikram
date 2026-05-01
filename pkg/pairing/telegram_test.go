package pairing

import (
	"testing"
	"time"
)

func TestTelegramStoreCreateOrReuseAndApprove(t *testing.T) {
	t.Setenv("LEVIK_HOME", t.TempDir())

	store := NewTelegramStore()

	first, err := store.CreateOrReuse("123|alice", "123", "alice", "Alice")
	if err != nil {
		t.Fatalf("CreateOrReuse() error = %v", err)
	}
	if first.OTP == "" {
		t.Fatal("CreateOrReuse() returned empty OTP")
	}

	second, err := store.CreateOrReuse("123|alice", "123", "alice", "Alice")
	if err != nil {
		t.Fatalf("CreateOrReuse() second error = %v", err)
	}
	if first.OTP != second.OTP {
		t.Fatalf("expected same OTP for same sender, got %q and %q", first.OTP, second.OTP)
	}

	reqs, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("List() len = %d, want 1", len(reqs))
	}

	approved, err := store.Approve(first.OTP)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved.SenderID != "123|alice" {
		t.Fatalf("Approve() sender = %q, want %q", approved.SenderID, "123|alice")
	}

	reqs, err = store.List()
	if err != nil {
		t.Fatalf("List() after approve error = %v", err)
	}
	if len(reqs) != 0 {
		t.Fatalf("List() after approve len = %d, want 0", len(reqs))
	}
}

func TestTelegramStoreAtUsesExplicitHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LEVIK_HOME", t.TempDir())

	store := NewTelegramStoreAt(home)
	req, err := store.CreateOrReuse("123|alice", "123", "alice", "Alice")
	if err != nil {
		t.Fatalf("CreateOrReuse() error = %v", err)
	}

	approved, err := store.Approve(req.OTP)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if approved.SenderID != "123|alice" {
		t.Fatalf("Approve() sender = %q, want %q", approved.SenderID, "123|alice")
	}
}

func TestTelegramStorePrunesExpiredRequests(t *testing.T) {
	t.Setenv("LEVIK_HOME", t.TempDir())

	store := NewTelegramStore()
	if err := store.save([]TelegramRequest{
		{
			OTP:       "111111",
			SenderID:  "old",
			CreatedAt: time.Now().Add(-time.Hour),
			ExpiresAt: time.Now().Add(-time.Minute),
		},
		{
			OTP:       "222222",
			SenderID:  "new",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Minute),
		},
	}); err != nil {
		t.Fatalf("seed save error = %v", err)
	}

	reqs, err := store.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("List() len = %d, want 1", len(reqs))
	}
	if reqs[0].SenderID != "new" {
		t.Fatalf("List() sender = %q, want %q", reqs[0].SenderID, "new")
	}
}

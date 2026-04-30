package permissions

import (
	"testing"
)

func TestDenyByDefault(t *testing.T) {
	r := NewRegistry()
	for _, f := range AllFeatures() {
		if r.IsAllowed(f) {
			t.Errorf("%s should be denied by default", f)
		}
	}
}

func TestSetAndCheck(t *testing.T) {
	r := NewRegistry()

	_ = r.Set(Camera, true)
	if !r.IsAllowed(Camera) {
		t.Error("camera should be allowed after Set(true)")
	}

	_ = r.Set(Camera, false)
	if r.IsAllowed(Camera) {
		t.Error("camera should be denied after Set(false)")
	}
}

func TestCheckReturnsError(t *testing.T) {
	r := NewRegistry()

	err := r.Check(SMS, "test")
	if err == nil {
		t.Error("expected error for denied permission")
	}

	_ = r.Set(SMS, true)
	err = r.Check(SMS, "test")
	if err != nil {
		t.Errorf("expected nil for allowed permission, got %v", err)
	}
}

func TestSetAll(t *testing.T) {
	r := NewRegistry()
	_ = r.SetAll(map[Feature]bool{
		Camera:     true,
		Microphone: true,
		SMS:        false,
	})

	if !r.IsAllowed(Camera) {
		t.Error("camera should be allowed")
	}
	if !r.IsAllowed(Microphone) {
		t.Error("microphone should be allowed")
	}
	if r.IsAllowed(SMS) {
		t.Error("sms should be denied")
	}
	if r.IsAllowed(Location) {
		t.Error("location should be denied (not set)")
	}
}

func TestSnapshot(t *testing.T) {
	r := NewRegistry()
	_ = r.Set(Location, true)

	snap := r.Snapshot()
	if !snap[Location] {
		t.Error("snapshot should show location=true")
	}

	// Mutating snapshot shouldn't affect registry.
	snap[Location] = false
	if !r.IsAllowed(Location) {
		t.Error("registry should not be affected by snapshot mutation")
	}
}

func TestEnabledFeatures(t *testing.T) {
	r := NewRegistry()
	_ = r.Set(Camera, true)
	_ = r.Set(Microphone, true)

	enabled := r.EnabledFeatures()
	if len(enabled) != 2 {
		t.Errorf("expected 2 enabled, got %d", len(enabled))
	}
}

func TestGlobalSingleton(t *testing.T) {
	g1 := Global()
	g2 := Global()
	if g1 != g2 {
		t.Error("Global() should return same instance")
	}
}

func TestAllFeatures(t *testing.T) {
	features := AllFeatures()
	if len(features) != 10 {
		t.Errorf("expected 10 features, got %d", len(features))
	}
}

func TestFreeze(t *testing.T) {
	r := NewRegistry()

	// Set before freeze should work.
	if err := r.Set(Camera, true); err != nil {
		t.Fatalf("Set before freeze should succeed: %v", err)
	}
	if !r.IsAllowed(Camera) {
		t.Error("camera should be allowed")
	}

	// Freeze the registry.
	r.Freeze()
	if !r.IsFrozen() {
		t.Error("registry should be frozen after Freeze()")
	}

	// Set after freeze should fail.
	if err := r.Set(Microphone, true); err == nil {
		t.Error("Set after freeze should return error")
	}
	if r.IsAllowed(Microphone) {
		t.Error("microphone should not be allowed after frozen Set")
	}

	// SetAll after freeze should fail.
	if err := r.SetAll(map[Feature]bool{SMS: true}); err == nil {
		t.Error("SetAll after freeze should return error")
	}
	if r.IsAllowed(SMS) {
		t.Error("sms should not be allowed after frozen SetAll")
	}
}

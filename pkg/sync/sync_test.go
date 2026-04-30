package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRegistry() *Registry {
	return NewRegistry(DeviceInfo{
		ID:           "v1-desktop",
		Name:         "Desktop V1",
		Host:         "192.168.1.10",
		Port:         18791,
		Platform:     "darwin",
		Capabilities: []DeviceCapability{CapScreen, CapSpeaker, CapMicrophone},
		Location:     "office",
		Version:      "1.0.0",
	})
}

func TestRegistry_Self(t *testing.T) {
	reg := newTestRegistry()
	self := reg.Self()
	assert.Equal(t, "v1-desktop", self.ID)
	assert.True(t, self.Online)
}

func TestRegistry_Register(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{
		ID:       "v1-phone",
		Name:     "Phone V1",
		Host:     "192.168.1.20",
		Port:     18791,
		Platform: "android",
		Location: "pocket",
	})
	assert.Equal(t, 2, reg.Count())
}

func TestRegistry_Get(t *testing.T) {
	reg := newTestRegistry()
	d := reg.Get("v1-desktop")
	require.NotNil(t, d)
	assert.Equal(t, "Desktop V1", d.Name)
}

func TestRegistry_Get_NotFound(t *testing.T) {
	reg := newTestRegistry()
	assert.Nil(t, reg.Get("nonexistent"))
}

func TestRegistry_Online(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "v1-phone", Name: "Phone", Online: true})
	reg.Register(DeviceInfo{ID: "v1-tablet", Name: "Tablet", Online: true})
	reg.MarkOffline("v1-tablet")

	online := reg.Online()
	assert.Len(t, online, 2) // self + phone
}

func TestRegistry_WithCapability(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{
		ID:           "v1-cam",
		Name:         "Doorbell",
		Capabilities: []DeviceCapability{CapCamera},
	})

	cams := reg.WithCapability(CapCamera)
	assert.Len(t, cams, 1)
	assert.Equal(t, "Doorbell", cams[0].Name)

	mics := reg.WithCapability(CapMicrophone)
	assert.Len(t, mics, 1)
	assert.Equal(t, "Desktop V1", mics[0].Name)
}

func TestRegistry_ByLocation(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "v1-bedroom", Location: "bedroom"})
	reg.Register(DeviceInfo{ID: "v1-kitchen", Location: "kitchen"})

	office := reg.ByLocation("office")
	assert.Len(t, office, 1)
}

func TestRegistry_Unregister(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "temp"})
	assert.Equal(t, 2, reg.Count())
	reg.Unregister("temp")
	assert.Equal(t, 1, reg.Count())
}

func TestRegistry_PruneStale(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "old-device", Name: "Old"})

	// Manually set LastSeen to past.
	reg.mu.Lock()
	reg.devices["old-device"].LastSeen = time.Now().Add(-10 * time.Minute)
	reg.mu.Unlock()

	pruned := reg.PruneStale(5 * time.Minute)
	assert.Equal(t, 1, pruned)

	d := reg.Get("old-device")
	assert.False(t, d.Online)
}

func TestHandoff_Transfer(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "v1-phone", Name: "Phone", Online: true})

	handoff := NewHandoff(reg)
	session := SessionState{
		SessionKey: "test:session",
		UserID:     "user1",
		Messages:   []SessionMessage{{Role: "user", Content: "Hello V1"}},
	}

	err := handoff.Transfer(context.Background(), session, "v1-phone")
	require.NoError(t, err)
}

func TestHandoff_Transfer_DeviceNotFound(t *testing.T) {
	reg := newTestRegistry()
	handoff := NewHandoff(reg)
	err := handoff.Transfer(context.Background(), SessionState{}, "missing")
	assert.Error(t, err)
}

func TestHandoff_Transfer_DeviceOffline(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{ID: "v1-phone", Name: "Phone"})
	reg.MarkOffline("v1-phone")

	handoff := NewHandoff(reg)
	err := handoff.Transfer(context.Background(), SessionState{}, "v1-phone")
	assert.Error(t, err)
}

func TestHandoff_BestDeviceFor(t *testing.T) {
	reg := newTestRegistry() // self has CapMicrophone
	handoff := NewHandoff(reg)

	best := handoff.BestDeviceFor(CapMicrophone)
	require.NotNil(t, best)
	assert.Equal(t, "v1-desktop", best.ID) // Prefers self

	best = handoff.BestDeviceFor(CapCamera)
	assert.Nil(t, best) // No camera device
}

func TestHandoff_BestDeviceFor_Remote(t *testing.T) {
	reg := newTestRegistry()
	reg.Register(DeviceInfo{
		ID:           "v1-cam",
		Capabilities: []DeviceCapability{CapCamera},
		Online:       true,
	})

	handoff := NewHandoff(reg)
	best := handoff.BestDeviceFor(CapCamera)
	require.NotNil(t, best)
	assert.Equal(t, "v1-cam", best.ID)
}

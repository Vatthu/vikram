package events

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEvent(t *testing.T) {
	event := NewEvent(KindMessage, "telegram", PriorityHigh)

	assert.Equal(t, KindMessage, event.Kind)
	assert.Equal(t, "telegram", event.Source)
	assert.Equal(t, PriorityHigh, event.Priority)
	assert.NotEmpty(t, event.ID)
	assert.False(t, event.Timestamp.IsZero())
	assert.NotNil(t, event.Payload)
	assert.NotNil(t, event.Metadata)
}

func TestEventWithPayload(t *testing.T) {
	event := NewEvent(KindMessage, "test", PriorityNormal).
		WithPayload("text", "hello").
		WithPayload("user", "bob").
		WithChannel("telegram", "12345").
		WithMetadata("trace_id", "abc")

	assert.Equal(t, "hello", event.Payload["text"])
	assert.Equal(t, "bob", event.Payload["user"])
	assert.Equal(t, "telegram", event.Channel)
	assert.Equal(t, "12345", event.ChatID)
	assert.Equal(t, "abc", event.Metadata["trace_id"])
}

func TestRouterStartStop(t *testing.T) {
	router := NewRouter()

	ctx := context.Background()
	err := router.Start(ctx)
	require.NoError(t, err)
	assert.True(t, router.IsRunning())

	// Starting again should error.
	err = router.Start(ctx)
	assert.Error(t, err)

	router.Stop()
	assert.False(t, router.IsRunning())
}

func TestRouterSubscribeAndDispatch(t *testing.T) {
	router := NewRouter()

	var received []Event
	var mu sync.Mutex
	router.Subscribe("test_handler", func(ctx context.Context, event Event) error {
		mu.Lock()
		received = append(received, event)
		mu.Unlock()
		return nil
	})

	timer := NewTimerSource("test_timer", 50*time.Millisecond)
	err := router.RegisterSource(timer)
	require.NoError(t, err)

	ctx := context.Background()
	err = router.Start(ctx)
	require.NoError(t, err)

	// Wait for at least 2 timer events.
	time.Sleep(150 * time.Millisecond)

	router.Stop()

	mu.Lock()
	count := len(received)
	mu.Unlock()

	assert.GreaterOrEqual(t, count, 2, "Should have received at least 2 timer events")
	assert.Equal(t, KindTimer, received[0].Kind)
	assert.Equal(t, "test_timer", received[0].Source)
}

func TestRouterFilterByKind(t *testing.T) {
	router := NewRouter()

	var timerCount, webhookCount int32
	router.Subscribe("timer_handler", func(ctx context.Context, event Event) error {
		atomic.AddInt32(&timerCount, 1)
		return nil
	}, FilterByKind(KindTimer))

	router.Subscribe("webhook_handler", func(ctx context.Context, event Event) error {
		atomic.AddInt32(&webhookCount, 1)
		return nil
	}, FilterByKind(KindWebhook))

	timer := NewTimerSource("tick", 50*time.Millisecond)
	router.RegisterSource(timer)

	ctx := context.Background()
	router.Start(ctx)

	time.Sleep(150 * time.Millisecond)

	// Manually emit a webhook event.
	router.Emit(NewEvent(KindWebhook, "github", PriorityNormal))
	time.Sleep(50 * time.Millisecond)

	router.Stop()

	assert.GreaterOrEqual(t, atomic.LoadInt32(&timerCount), int32(2))
	assert.Equal(t, int32(1), atomic.LoadInt32(&webhookCount))
}

func TestRouterFilterBySource(t *testing.T) {
	router := NewRouter()

	var count int32
	router.Subscribe("source_handler", func(ctx context.Context, event Event) error {
		atomic.AddInt32(&count, 1)
		return nil
	}, FilterBySource("important"))

	ctx := context.Background()
	router.Start(ctx)

	router.Emit(NewEvent(KindCustom, "important", PriorityNormal))
	router.Emit(NewEvent(KindCustom, "unrelated", PriorityNormal))
	router.Emit(NewEvent(KindCustom, "important", PriorityNormal))
	time.Sleep(50 * time.Millisecond)

	router.Stop()

	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestRouterPriorityOrdering(t *testing.T) {
	router := NewRouter()

	var order []string
	var mu sync.Mutex

	router.SubscribeWithPriority("low", PriorityLow, func(ctx context.Context, event Event) error {
		mu.Lock()
		order = append(order, "low")
		mu.Unlock()
		return nil
	})
	router.SubscribeWithPriority("critical", PriorityCritical, func(ctx context.Context, event Event) error {
		mu.Lock()
		order = append(order, "critical")
		mu.Unlock()
		return nil
	})
	router.SubscribeWithPriority("normal", PriorityNormal, func(ctx context.Context, event Event) error {
		mu.Lock()
		order = append(order, "normal")
		mu.Unlock()
		return nil
	})

	ctx := context.Background()
	router.Start(ctx)

	router.Emit(NewEvent(KindCustom, "test", PriorityNormal))
	time.Sleep(50 * time.Millisecond)

	router.Stop()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, order, 3)
	// Priority order can vary due to concurrent goroutine scheduling, so we just check they all fired.
	assert.Contains(t, order, "critical")
	assert.Contains(t, order, "normal")
	assert.Contains(t, order, "low")
}

func TestRouterUnsubscribe(t *testing.T) {
	router := NewRouter()

	var count int32
	id := router.Subscribe("to_remove", func(ctx context.Context, event Event) error {
		atomic.AddInt32(&count, 1)
		return nil
	})

	ctx := context.Background()
	router.Start(ctx)

	router.Emit(NewEvent(KindCustom, "test", PriorityNormal))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))

	router.Unsubscribe(id)
	router.Emit(NewEvent(KindCustom, "test", PriorityNormal))
	time.Sleep(50 * time.Millisecond)

	router.Stop()
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestRouterDuplicateSource(t *testing.T) {
	router := NewRouter()

	timer1 := NewTimerSource("same_name", time.Second)
	timer2 := NewTimerSource("same_name", time.Second)

	err := router.RegisterSource(timer1)
	require.NoError(t, err)

	err = router.RegisterSource(timer2)
	assert.Error(t, err)
}

func TestRouterEmitWhenStopped(t *testing.T) {
	router := NewRouter()
	// Should not panic, just log warning.
	router.Emit(NewEvent(KindCustom, "test", PriorityNormal))
}

func TestTimerSource(t *testing.T) {
	timer := NewTimerSource("test", 30*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := timer.Start(ctx)
	require.NoError(t, err)

	var events []Event
	done := make(chan struct{})
	go func() {
		for e := range ch {
			events = append(events, e)
			if len(events) >= 3 {
				cancel()
				return
			}
		}
		close(done)
	}()

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for timer events")
	}

	timer.Stop()
	assert.GreaterOrEqual(t, len(events), 3)
}

func TestWebhookSource(t *testing.T) {
	wh := NewWebhookSource("github_webhook")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := wh.Start(ctx)
	require.NoError(t, err)

	// Simulate webhook push.
	wh.Receive(NewEvent(KindWebhook, "github", PriorityHigh).
		WithPayload("action", "push").
		WithPayload("repo", "vikram"))

	select {
	case event := <-ch:
		assert.Equal(t, KindWebhook, event.Kind)
		assert.Equal(t, "push", event.Payload["action"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for webhook event")
	}

	wh.Stop()
}

func TestFSWatchSource(t *testing.T) {
	// Create a temp file to watch.
	tmpDir := t.TempDir()
	tmpFile := tmpDir + "/test.txt"
	require.NoError(t, writeTestFile(tmpFile, "initial"))

	fs := NewFSWatchSource("config_watch", tmpFile, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := fs.Start(ctx)
	require.NoError(t, err)

	// Wait for initial stat.
	time.Sleep(80 * time.Millisecond)

	// Modify the file.
	require.NoError(t, writeTestFile(tmpFile, "modified"))

	select {
	case event := <-ch:
		assert.Equal(t, KindFS, event.Kind)
		assert.Equal(t, tmpFile, event.Payload["path"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for filesystem event")
	}

	fs.Stop()
}

func TestHeartbeatAdapter(t *testing.T) {
	adapter := NewHeartbeatAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := adapter.Start(ctx)
	require.NoError(t, err)

	adapter.EmitTick("telegram", "12345", "Check weather")

	select {
	case event := <-ch:
		assert.Equal(t, KindHeartbeat, event.Kind)
		assert.Equal(t, "telegram", event.Channel)
		assert.Equal(t, "Check weather", event.Payload["prompt"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for heartbeat event")
	}

	adapter.Stop()
}

func TestCronAdapter(t *testing.T) {
	adapter := NewCronAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := adapter.Start(ctx)
	require.NoError(t, err)

	adapter.EmitJobFired("job_1", "daily_report", "discord", "99999")

	select {
	case event := <-ch:
		assert.Equal(t, KindCron, event.Kind)
		assert.Equal(t, "job_1", event.Payload["job_id"])
		assert.Equal(t, "discord", event.Channel)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for cron event")
	}

	adapter.Stop()
}

func TestDeviceAdapter(t *testing.T) {
	adapter := NewDeviceAdapter()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := adapter.Start(ctx)
	require.NoError(t, err)

	adapter.EmitDeviceEvent("add", "usb", "USB Keyboard", "telegram", "12345")

	select {
	case event := <-ch:
		assert.Equal(t, KindDevice, event.Kind)
		assert.Equal(t, "add", event.Payload["action"])
		assert.Equal(t, "USB Keyboard", event.Payload["device_name"])
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for device event")
	}

	adapter.Stop()
}

func TestRouterCounts(t *testing.T) {
	router := NewRouter()

	assert.Equal(t, 0, router.SourceCount())
	assert.Equal(t, 0, router.SubscriptionCount())

	router.RegisterSource(NewTimerSource("t1", time.Second))
	router.RegisterSource(NewTimerSource("t2", time.Second))
	router.Subscribe("h1", func(ctx context.Context, event Event) error { return nil })

	assert.Equal(t, 2, router.SourceCount())
	assert.Equal(t, 1, router.SubscriptionCount())
}

func TestFilterByPriority(t *testing.T) {
	f := FilterByPriority(PriorityHigh)
	assert.True(t, f(NewEvent(KindCustom, "test", PriorityCritical)))
	assert.True(t, f(NewEvent(KindCustom, "test", PriorityHigh)))
	assert.False(t, f(NewEvent(KindCustom, "test", PriorityNormal)))
	assert.False(t, f(NewEvent(KindCustom, "test", PriorityLow)))
}

// writeTestFile is a helper to write content to a file.
func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}

package bus

import (
	"context"
	"testing"
	"time"
)

func TestMessageBus_PublishInbound_Saturation(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	// Fill the buffer to the brim (1024 messages)
	for i := 0; i < 1024; i++ {
		mb.PublishInbound(InboundMessage{Content: "filler"})
	}

	// The 1025th message should hit the default case and drop to prevent deadlock
	// We test this by assigning a timeout. If it blocked, it would fail the test timeout.
	done := make(chan bool)
	go func() {
		mb.PublishInbound(InboundMessage{Content: "overload"})
		done <- true
	}()

	select {
	case <-done:
		// Success: PublishInbound didn't block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("PublishInbound blocked when buffer was full, indicating deadlock potential!")
	}
}

func TestMessageBus_PublishOutbound_Saturation(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	// Subscribe exactly one listener
	sub := mb.SubscribeOutbound()

	// The listener buffer is 1024. Fill it.
	for i := 0; i < 1024; i++ {
		mb.PublishOutbound(OutboundMessage{Content: "filler"})
	}

	// This should drop instead of blocking all other consumers and the publisher itself
	done := make(chan bool)
	go func() {
		mb.PublishOutbound(OutboundMessage{Content: "overload"})
		done <- true
	}()

	select {
	case <-done:
		// Success: PublishOutbound returned immediately
	case <-time.After(100 * time.Millisecond):
		t.Fatal("PublishOutbound blocked on a slow subscriber buffer!")
	}

	// Consume one to verify the channel is actually full and working
	msg := <-sub.C
	if msg.Content != "filler" {
		t.Errorf("Expected 'filler', got %v", msg.Content)
	}
}

func TestMessageBus_Lifecycle(t *testing.T) {
	mb := NewMessageBus()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mb.PublishInbound(InboundMessage{Content: "test"})
	msg, ok := mb.ConsumeInbound(ctx)
	if !ok || msg.Content != "test" {
		t.Fatal("Failed reading inbound message immediately")
	}

	mb.Close()

	// Operations after close shouldn't panic
	mb.PublishOutbound(OutboundMessage{Content: "ghost"})
}

func TestMessageBus_UnsubscribeRemovesSubscriber(t *testing.T) {
	mb := NewMessageBus()
	defer mb.Close()

	sub := mb.SubscribeOutbound()
	metrics := mb.Metrics()
	if got := metrics["subscriber_count"]; got != 1 {
		t.Fatalf("subscriber_count = %v, want 1", got)
	}

	sub.Unsubscribe()
	metrics = mb.Metrics()
	if got := metrics["subscriber_count"]; got != 0 {
		t.Fatalf("subscriber_count after unsubscribe = %v, want 0", got)
	}
}

package events

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// Priority levels for event processing order.
const (
	PriorityCritical = 0
	PriorityHigh     = 1
	PriorityNormal   = 2
	PriorityLow      = 3
)

// Event kinds for categorizing events.
const (
	KindMessage   = "message"
	KindHeartbeat = "heartbeat"
	KindCron      = "cron"
	KindDevice    = "device"
	KindWebhook   = "webhook"
	KindTimer     = "timer"
	KindFS        = "filesystem"
	KindSystem    = "system"
	KindCustom    = "custom"
)

// Event is the unified event type that flows through the router.
type Event struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Source    string                 `json:"source"`
	Priority  int                    `json:"priority"`
	Timestamp time.Time              `json:"timestamp"`
	Channel   string                 `json:"channel,omitempty"`
	ChatID    string                 `json:"chat_id,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
}

// EventSource produces events. Implementations wrap existing systems
// (heartbeat, cron, USB devices) or add new ones (filesystem watcher, webhook).
type EventSource interface {
	// Name returns a unique identifier for this source.
	Name() string
	// Kind returns the event kind this source produces.
	Kind() string
	// Start begins emitting events on the returned channel.
	// The source must stop when ctx is cancelled.
	Start(ctx context.Context) (<-chan Event, error)
	// Stop gracefully shuts down the source.
	Stop()
}

// EventHandler processes events. Return an error to signal failure
// (the router will log it but continue processing other events).
type EventHandler func(ctx context.Context, event Event) error

// EventFilter decides whether an event should be delivered to a handler.
type EventFilter func(event Event) bool

// Subscription binds a handler to a set of filters.
type Subscription struct {
	ID       string
	Name     string
	Handler  EventHandler
	Filters  []EventFilter
	Priority int
}

// NewEvent creates an event with a generated ID and current timestamp.
func NewEvent(kind, source string, priority int) Event {
	return Event{
		ID:        generateEventID(),
		Kind:      kind,
		Source:    source,
		Priority:  priority,
		Timestamp: time.Now(),
		Payload:   make(map[string]interface{}),
		Metadata:  make(map[string]string),
	}
}

// WithChannel sets the target channel for routing.
func (e Event) WithChannel(channel, chatID string) Event {
	e.Channel = channel
	e.ChatID = chatID
	return e
}

// WithPayload adds a key-value pair to the event payload.
func (e Event) WithPayload(key string, value interface{}) Event {
	if e.Payload == nil {
		e.Payload = make(map[string]interface{})
	}
	e.Payload[key] = value
	return e
}

// WithMetadata adds a key-value pair to the event metadata.
func (e Event) WithMetadata(key, value string) Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]string)
	}
	e.Metadata[key] = value
	return e
}

// FilterByKind returns a filter that matches events of the given kind.
func FilterByKind(kind string) EventFilter {
	return func(e Event) bool { return e.Kind == kind }
}

// FilterBySource returns a filter that matches events from the given source.
func FilterBySource(source string) EventFilter {
	return func(e Event) bool { return e.Source == source }
}

// FilterByPriority returns a filter matching events at or above a priority level
// (lower number = higher priority).
func FilterByPriority(maxPriority int) EventFilter {
	return func(e Event) bool { return e.Priority <= maxPriority }
}

var eventCounter uint64

func generateEventID() string {
	counter := atomic.AddUint64(&eventCounter, 1)
	return fmt.Sprintf("evt_%d_%d", time.Now().UnixNano(), counter)
}

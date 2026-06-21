package events

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/logger"
)

const logComponent = "events"

// Router is the central event dispatcher. It collects events from multiple
// sources and routes them to matching subscriptions ordered by priority.
type Router struct {
	mu            sync.RWMutex
	sources       map[string]EventSource
	subscriptions map[string]*Subscription
	queue         *eventQueue
	cancel        context.CancelFunc
	routerCtx     context.Context // Persistent context for all handlers
	running       bool
	wg            sync.WaitGroup
	semaphore     chan struct{} // Limits concurrent handlers
}

// NewRouter creates a new event router.
func NewRouter() *Router {
	eq := &eventQueue{}
	heap.Init(eq)
	return &Router{
		sources:       make(map[string]EventSource),
		subscriptions: make(map[string]*Subscription),
		queue:         eq,
		semaphore:     make(chan struct{}, 100), // Maximum 100 concurrent handlers
	}
}

// RegisterSource adds an event source to the router. Must be called before Start.
func (r *Router) RegisterSource(source EventSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := source.Name()
	if _, exists := r.sources[name]; exists {
		return fmt.Errorf("event source %q already registered", name)
	}
	r.sources[name] = source
	logger.InfoC(logComponent, fmt.Sprintf("Registered source: %s (kind=%s)", name, source.Kind()))
	return nil
}

// Subscribe registers a handler to receive events matching the given filters.
// If no filters are provided, the handler receives all events.
func (r *Router) Subscribe(name string, handler EventHandler, filters ...EventFilter) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := fmt.Sprintf("sub_%s_%d", name, len(r.subscriptions))
	r.subscriptions[id] = &Subscription{
		ID:       id,
		Name:     name,
		Handler:  handler,
		Filters:  filters,
		Priority: PriorityNormal,
	}
	logger.DebugC(logComponent, fmt.Sprintf("Subscribed: %s (id=%s, filters=%d)", name, id, len(filters)))
	return id
}

// SubscribeWithPriority is like Subscribe but allows setting a handler priority.
func (r *Router) SubscribeWithPriority(name string, priority int, handler EventHandler, filters ...EventFilter) string {
	r.mu.Lock()
	defer r.mu.Unlock()

	id := fmt.Sprintf("sub_%s_%d", name, len(r.subscriptions))
	r.subscriptions[id] = &Subscription{
		ID:       id,
		Name:     name,
		Handler:  handler,
		Filters:  filters,
		Priority: priority,
	}
	logger.DebugC(logComponent, fmt.Sprintf("Subscribed: %s (id=%s, priority=%d)", name, id, priority))
	return id
}

// Unsubscribe removes a subscription by ID.
func (r *Router) Unsubscribe(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.subscriptions, id)
	logger.DebugC(logComponent, fmt.Sprintf("Unsubscribed: %s", id))
}

// Emit manually injects an event into the router (useful for webhook/API events).
func (r *Router) Emit(event Event) {
	r.mu.RLock()
	running := r.running
	r.mu.RUnlock()

	if !running {
		logger.WarnC(logComponent, fmt.Sprintf("Event dropped (router not running): %s/%s", event.Kind, event.Source))
		return
	}

	r.dispatch(event)
}

// Start begins collecting events from all registered sources and dispatching them.
func (r *Router) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return fmt.Errorf("router already running")
	}

	r.routerCtx, r.cancel = context.WithCancel(ctx)
	r.running = true
	r.mu.Unlock()

	// Start all event sources and fan-in their event channels.
	for name, source := range r.sources {
		ch, err := source.Start(r.routerCtx)
		if err != nil {
			logger.ErrorC(logComponent, fmt.Sprintf("Failed to start source %s: %v", name, err))
			continue
		}
		r.wg.Add(1)
		go r.collectFromSource(r.routerCtx, name, ch)
	}

	logger.InfoC(logComponent, fmt.Sprintf("Router started with %d sources and %d subscriptions",
		len(r.sources), len(r.subscriptions)))
	return nil
}

// Stop gracefully shuts down the router and all sources.
func (r *Router) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	r.mu.Unlock()

	if r.cancel != nil {
		r.cancel()
	}

	// Stop all sources explicitly.
	for name, source := range r.sources {
		logger.DebugC(logComponent, fmt.Sprintf("Stopping source: %s", name))
		source.Stop()
	}

	r.wg.Wait()
	logger.InfoC(logComponent, "Router stopped")
}

// IsRunning returns whether the router is active.
func (r *Router) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// SourceCount returns the number of registered sources.
func (r *Router) SourceCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.sources)
}

// SubscriptionCount returns the number of active subscriptions.
func (r *Router) SubscriptionCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.subscriptions)
}

// collectFromSource reads events from a source channel and dispatches them.
func (r *Router) collectFromSource(ctx context.Context, name string, ch <-chan Event) {
	defer r.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				logger.DebugC(logComponent, fmt.Sprintf("Source channel closed: %s", name))
				return
			}
			r.dispatch(event)
		}
	}
}

// dispatch routes an event to all matching subscriptions.
// It executes handlers concurrently to avoid blocking other events.
func (r *Router) dispatch(event Event) {
	r.mu.RLock()
	var matched []*Subscription
	for _, sub := range r.subscriptions {
		if r.matchesFilters(event, sub.Filters) {
			matched = append(matched, sub)
		}
	}
	// Capture the current router context
	ctx := r.routerCtx
	r.mu.RUnlock()

	if len(matched) == 0 {
		logger.DebugC(logComponent, fmt.Sprintf("No handlers for event: %s/%s (id=%s)", event.Kind, event.Source, event.ID))
		return
	}

	// Sort by subscription priority (lower = higher priority).
	sortSubscriptions(matched)

	// Execute handlers concurrently with a semaphore limit explicitly bounding goroutine creation
	for _, sub := range matched {
		r.wg.Add(1)

		go func(s *Subscription, e Event) {
			defer r.wg.Done()
			// Acquire semaphore inside the goroutine so the dispatch caller is never
			// blocked.  Use a select so the goroutine exits cleanly on router shutdown
			// instead of blocking indefinitely when all 100 slots are taken.
			select {
			case r.semaphore <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-r.semaphore }()

			// Create a task-specific context with a timeout derived from the router context
			hCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			if err := s.Handler(hCtx, e); err != nil {
				logger.ErrorC(logComponent, fmt.Sprintf("Handler %s failed for event %s: %v", s.Name, e.ID, err))
			}
		}(sub, event)
	}
}

// matchesFilters returns true if the event passes all filters (AND logic).
// An empty filter list matches everything.
func (r *Router) matchesFilters(event Event, filters []EventFilter) bool {
	for _, f := range filters {
		if !f(event) {
			return false
		}
	}
	return true
}

// sortSubscriptions sorts by priority (lower number first).
func sortSubscriptions(subs []*Subscription) {
	for i := 1; i < len(subs); i++ {
		for j := i; j > 0 && subs[j].Priority < subs[j-1].Priority; j-- {
			subs[j], subs[j-1] = subs[j-1], subs[j]
		}
	}
}

// eventQueue implements heap.Interface for priority-ordered event processing.
type eventQueue []Event

func (eq eventQueue) Len() int           { return len(eq) }
func (eq eventQueue) Less(i, j int) bool { return eq[i].Priority < eq[j].Priority }
func (eq eventQueue) Swap(i, j int)      { eq[i], eq[j] = eq[j], eq[i] }

func (eq *eventQueue) Push(x interface{}) {
	*eq = append(*eq, x.(Event))
}

func (eq *eventQueue) Pop() interface{} {
	old := *eq
	n := len(old)
	item := old[n-1]
	*eq = old[:n-1]
	return item
}

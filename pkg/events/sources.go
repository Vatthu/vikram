package events

import (
	"context"
	"os"
	"time"
)

// TimerSource emits events at a fixed interval.
type TimerSource struct {
	name     string
	interval time.Duration
	payload  map[string]interface{}
	stopCh   chan struct{}
}

// NewTimerSource creates a source that fires events at the given interval.
func NewTimerSource(name string, interval time.Duration) *TimerSource {
	return &TimerSource{
		name:     name,
		interval: interval,
		payload:  make(map[string]interface{}),
	}
}

func (t *TimerSource) Name() string { return t.name }
func (t *TimerSource) Kind() string { return KindTimer }

func (t *TimerSource) Start(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 10)
	t.stopCh = make(chan struct{})
	go func() {
		defer close(ch)
		ticker := time.NewTicker(t.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.stopCh:
				return
			case <-ticker.C:
				event := NewEvent(KindTimer, t.name, PriorityNormal).
					WithPayload("interval_seconds", t.interval.Seconds())
				for k, v := range t.payload {
					event = event.WithPayload(k, v)
				}
				select {
				case ch <- event:
				default:
					// Drop event if channel full to avoid blocking.
				}
			}
		}
	}()
	return ch, nil
}

func (t *TimerSource) Stop() {
	if t.stopCh != nil {
		select {
		case <-t.stopCh:
		default:
			close(t.stopCh)
		}
	}
}

// WebhookSource receives events from external HTTP webhook calls.
// Events are pushed via the Receive method (called from an HTTP handler).
type WebhookSource struct {
	name   string
	ch     chan Event
	stopCh chan struct{}
}

// NewWebhookSource creates a source that receives events via Receive().
func NewWebhookSource(name string) *WebhookSource {
	return &WebhookSource{
		name: name,
		ch:   make(chan Event, 50),
	}
}

func (w *WebhookSource) Name() string { return w.name }
func (w *WebhookSource) Kind() string { return KindWebhook }

func (w *WebhookSource) Start(ctx context.Context) (<-chan Event, error) {
	w.stopCh = make(chan struct{})
	out := make(chan Event, 50)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stopCh:
				return
			case event, ok := <-w.ch:
				if !ok {
					return
				}
				select {
				case out <- event:
				default:
				}
			}
		}
	}()
	return out, nil
}

func (w *WebhookSource) Stop() {
	if w.stopCh != nil {
		select {
		case <-w.stopCh:
		default:
			close(w.stopCh)
		}
	}
}

// Receive pushes an event from an external webhook HTTP handler.
func (w *WebhookSource) Receive(event Event) {
	select {
	case w.ch <- event:
	default:
	}
}

// FSWatchSource monitors a file or directory for changes using polling.
// Lightweight alternative to fsnotify (no CGo, works on all platforms).
type FSWatchSource struct {
	name     string
	path     string
	interval time.Duration
	stopCh   chan struct{}
	lastMod  time.Time
}

// NewFSWatchSource creates a filesystem change watcher for the given path.
func NewFSWatchSource(name, path string, pollInterval time.Duration) *FSWatchSource {
	return &FSWatchSource{
		name:     name,
		path:     path,
		interval: pollInterval,
	}
}

func (f *FSWatchSource) Name() string { return f.name }
func (f *FSWatchSource) Kind() string { return KindFS }

func (f *FSWatchSource) Start(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 10)
	f.stopCh = make(chan struct{})
	go func() {
		defer close(ch)
		ticker := time.NewTicker(f.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-f.stopCh:
				return
			case <-ticker.C:
				modTime := fileModTime(f.path)
				if modTime.IsZero() {
					continue
				}
				if !f.lastMod.IsZero() && modTime.After(f.lastMod) {
					event := NewEvent(KindFS, f.name, PriorityNormal).
						WithPayload("path", f.path).
						WithPayload("mod_time", modTime.Format(time.RFC3339))
					select {
					case ch <- event:
					default:
					}
				}
				f.lastMod = modTime
			}
		}
	}()
	return ch, nil
}

func (f *FSWatchSource) Stop() {
	if f.stopCh != nil {
		select {
		case <-f.stopCh:
		default:
			close(f.stopCh)
		}
	}
}

// fileModTime returns the modification time of a path, or zero if it fails.
func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

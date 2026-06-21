package bus

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vatthu/vikram/pkg/logger"
)

const (
	defaultInboundCap      = 1024
	defaultOutboundCap     = 256
	defaultSubscriberCap   = 1024
	subscriberWriteTimeout = 5 * time.Second
)

// MessageBus is a production-grade message bus with subscriber isolation,
// backpressure handling, bounded memory, and failure visibility.
//
// Architecture:
//   - Inbound: buffered channel with non-blocking send (drop on full)
//   - Outbound: per-subscriber goroutine with dedicated buffer; one slow
//     subscriber never blocks delivery to other subscribers
//   - Metrics: atomic counters track drops, deliveries, and subscriber lag
type MessageBus struct {
	inbound  chan InboundMessage
	handlers map[string]MessageHandler
	mu       sync.RWMutex

	// Outbound subscribers — each gets its own goroutine + buffer.
	subsMu    sync.RWMutex
	subs      []*subscriber
	nextID    int64
	done      chan struct{}
	closeOnce sync.Once

	// Metrics
	droppedInbound atomic.Int64
	deliveredTotal atomic.Int64
	droppedTotal   atomic.Int64
}

// subscriber is an isolated outbound consumer with its own goroutine.
type subscriber struct {
	id      int64
	mb      *MessageBus
	buf     chan OutboundMessage
	ctx     context.Context
	cancel  context.CancelFunc
	dropped atomic.Int64
}

// Subscription is the handle returned to outbound consumers.
type Subscription struct {
	C <-chan OutboundMessage
	s *subscriber
}

// Unsubscribe removes the subscription and stops its goroutine.
func (s *Subscription) Unsubscribe() {
	if s == nil || s.s == nil {
		return
	}
	s.s.mb.removeSubscriber(s.s)
}

func (mb *MessageBus) removeSubscriber(s *subscriber) {
	mb.subsMu.Lock()
	defer mb.subsMu.Unlock()
	for i, sub := range mb.subs {
		if sub == s {
			mb.subs = append(mb.subs[:i], mb.subs[i+1:]...)
			break
		}
	}
	s.cancel()
}

func NewMessageBus() *MessageBus {
	return &MessageBus{
		inbound:  make(chan InboundMessage, defaultInboundCap),
		handlers: make(map[string]MessageHandler),
		done:     make(chan struct{}),
	}
}

// ---- Inbound ----

// PublishInbound delivers a message to the agent loop.
// Returns true if queued, false if dropped (buffer full).
func (mb *MessageBus) PublishInbound(msg InboundMessage) bool {
	select {
	case <-mb.done:
		return false
	case mb.inbound <- msg:
		return true
	default:
		mb.droppedInbound.Add(1)
		logger.WarnCF("bus", "inbound buffer full, message dropped", map[string]interface{}{
			"channel":     msg.Channel,
			"sender_id":   msg.SenderID,
			"total_drops": mb.droppedInbound.Load(),
		})
		return false
	}
}

func (mb *MessageBus) ConsumeInbound(ctx context.Context) (InboundMessage, bool) {
	select {
	case msg, ok := <-mb.inbound:
		return msg, ok
	case <-ctx.Done():
		return InboundMessage{}, false
	}
}

// ---- Outbound ----

// PublishOutbound fans out to all subscribers concurrently.  Each subscriber
// gets its own goroutine + timeout, so a slow subscriber never blocks delivery
// to other subscribers.  Messages dropped due to subscriber buffer overflow are
// logged with the subscriber ID for diagnosis.
func (mb *MessageBus) PublishOutbound(msg OutboundMessage) {
	select {
	case <-mb.done:
		return
	default:
	}

	mb.subsMu.RLock()
	subs := append([]*subscriber(nil), mb.subs...)
	mb.subsMu.RUnlock()

	if len(subs) == 0 {
		return
	}

	var wg sync.WaitGroup
	wg.Add(len(subs))

	for _, s := range subs {
		go func(sub *subscriber) {
			defer wg.Done()
			select {
			case sub.buf <- msg:
				mb.deliveredTotal.Add(1)
			case <-sub.ctx.Done():
				mb.droppedTotal.Add(1)
			default:
				sub.dropped.Add(1)
				mb.droppedTotal.Add(1)
				logger.WarnCF("bus", "outbound subscriber buffer full, message dropped", map[string]interface{}{
					"subscriber_id": sub.id,
					"channel":       msg.Channel,
					"sub_drops":     sub.dropped.Load(),
					"total_drops":   mb.droppedTotal.Load(),
				})
			}
		}(s)
	}

	wg.Wait()
}

// SubscribeOutbound returns a Subscription whose C field delivers outbound
// messages.  Each subscriber gets its own goroutine that drains an internal
// buffer into the consumer-facing channel, providing isolation.
func (mb *MessageBus) SubscribeOutbound() *Subscription {
	mb.subsMu.Lock()
	defer mb.subsMu.Unlock()

	id := atomic.AddInt64(&mb.nextID, 1)
	buf := make(chan OutboundMessage, defaultSubscriberCap)

	ctx, cancel := context.WithCancel(context.Background())
	consumerCh := make(chan OutboundMessage, defaultOutboundCap)

	s := &subscriber{
		id:     id,
		buf:    buf,
		ctx:    ctx,
		cancel: cancel,
		mb:     mb,
	}

	// Drain goroutine: reads from the internal buffer and forwards to the
	// consumer channel with a timeout.  If the consumer is too slow, messages
	// are dropped rather than blocking the internal buffer.
	go func() {
		defer close(consumerCh)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-buf:
				if !ok {
					return
				}
				select {
				case consumerCh <- msg:
				case <-time.After(subscriberWriteTimeout):
					s.dropped.Add(1)
					mb.droppedTotal.Add(1)
					logger.WarnCF("bus", "subscriber consumer too slow, message dropped", map[string]interface{}{
						"subscriber_id": s.id,
						"sub_drops":     s.dropped.Load(),
					})
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	mb.subs = append(mb.subs, s)
	logger.InfoCF("bus", "subscriber attached", map[string]interface{}{
		"subscriber_id": id,
		"total_subs":    len(mb.subs),
	})

	return &Subscription{C: consumerCh, s: s}
}

// ---- Handlers ----

func (mb *MessageBus) RegisterHandler(channel string, handler MessageHandler) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.handlers[channel] = handler
}

func (mb *MessageBus) GetHandler(channel string) (MessageHandler, bool) {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	h, ok := mb.handlers[channel]
	return h, ok
}

// ---- Metrics ----

// Metrics returns a snapshot of bus health for observability.
func (mb *MessageBus) Metrics() map[string]interface{} {
	mb.subsMu.RLock()
	subMetrics := make([]map[string]interface{}, len(mb.subs))
	for i, s := range mb.subs {
		subMetrics[i] = map[string]interface{}{
			"id":      s.id,
			"dropped": s.dropped.Load(),
			"buf_len": len(s.buf),
		}
	}
	subscriberCount := len(mb.subs)
	mb.subsMu.RUnlock()

	return map[string]interface{}{
		"inbound_dropped":    mb.droppedInbound.Load(),
		"outbound_delivered": mb.deliveredTotal.Load(),
		"outbound_dropped":   mb.droppedTotal.Load(),
		"subscriber_count":   subscriberCount,
		"subscribers":        subMetrics,
	}
}

// Healthy returns an error if the bus has degraded (e.g., excessive drops).
func (mb *MessageBus) Healthy() error {
	drops := mb.droppedTotal.Load()
	if drops > 1000 {
		return fmt.Errorf("bus: %d outbound messages dropped", drops)
	}
	inboundDrops := mb.droppedInbound.Load()
	if inboundDrops > 100 {
		return fmt.Errorf("bus: %d inbound messages dropped", inboundDrops)
	}
	return nil
}

// ---- Lifecycle ----

func (mb *MessageBus) Close() {
	mb.closeOnce.Do(func() {
		close(mb.done)
		mb.subsMu.Lock()
		for _, s := range mb.subs {
			s.cancel()
		}
		mb.subs = nil
		mb.subsMu.Unlock()
	})
}

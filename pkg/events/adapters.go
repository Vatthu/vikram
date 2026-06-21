package events

import (
	"context"

	"github.com/Vatthu/vikram/pkg/bus"
)

// BusAdapter wraps the existing MessageBus as an EventSource,
// converting inbound messages into structured events.
type BusAdapter struct {
	name   string
	msgBus *bus.MessageBus
	stopCh chan struct{}
}

// NewBusAdapter creates an event source from the existing message bus.
func NewBusAdapter(msgBus *bus.MessageBus) *BusAdapter {
	return &BusAdapter{
		name:   "message_bus",
		msgBus: msgBus,
	}
}

func (b *BusAdapter) Name() string { return b.name }
func (b *BusAdapter) Kind() string { return KindMessage }

func (b *BusAdapter) Start(ctx context.Context) (<-chan Event, error) {
	ch := make(chan Event, 100)
	b.stopCh = make(chan struct{})

	go func() {
		defer close(ch)
		for {
			select {
			case <-ctx.Done():
				return
			case <-b.stopCh:
				return
			default:
				msg, ok := b.msgBus.ConsumeInbound(ctx)
				if !ok {
					return
				}
				event := NewEvent(KindMessage, msg.Channel, PriorityHigh).
					WithChannel(msg.Channel, msg.ChatID).
					WithPayload("text", msg.Content).
					WithPayload("sender_id", msg.SenderID).
					WithPayload("session_key", msg.SessionKey)
				if len(msg.Media) > 0 {
					event = event.WithPayload("media", msg.Media)
				}
				select {
				case ch <- event:
				default:
				}
			}
		}
	}()

	return ch, nil
}

func (b *BusAdapter) Stop() {
	if b.stopCh != nil {
		select {
		case <-b.stopCh:
		default:
			close(b.stopCh)
		}
	}
}

// HeartbeatAdapter wraps the heartbeat tick as an EventSource.
// It does NOT replace the HeartbeatService — it emits an event each time
// the heartbeat fires, so other handlers can react to it.
type HeartbeatAdapter struct {
	name     string
	eventsCh chan Event
	stopCh   chan struct{}
}

// NewHeartbeatAdapter creates an adapter that emits heartbeat events.
// Call EmitTick() from the existing HeartbeatService handler to bridge events.
func NewHeartbeatAdapter() *HeartbeatAdapter {
	return &HeartbeatAdapter{
		name:     "heartbeat",
		eventsCh: make(chan Event, 10),
	}
}

func (h *HeartbeatAdapter) Name() string { return h.name }
func (h *HeartbeatAdapter) Kind() string { return KindHeartbeat }

func (h *HeartbeatAdapter) Start(ctx context.Context) (<-chan Event, error) {
	out := make(chan Event, 10)
	h.stopCh = make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.stopCh:
				return
			case event, ok := <-h.eventsCh:
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

func (h *HeartbeatAdapter) Stop() {
	if h.stopCh != nil {
		select {
		case <-h.stopCh:
		default:
			close(h.stopCh)
		}
	}
}

// EmitTick is called by the existing HeartbeatService to bridge into the event system.
func (h *HeartbeatAdapter) EmitTick(channel, chatID, prompt string) {
	event := NewEvent(KindHeartbeat, "heartbeat", PriorityNormal).
		WithChannel(channel, chatID).
		WithPayload("prompt", prompt)
	select {
	case h.eventsCh <- event:
	default:
	}
}

// CronAdapter wraps cron job execution as events.
type CronAdapter struct {
	name     string
	eventsCh chan Event
	stopCh   chan struct{}
}

// NewCronAdapter creates an adapter that emits events when cron jobs fire.
func NewCronAdapter() *CronAdapter {
	return &CronAdapter{
		name:     "cron",
		eventsCh: make(chan Event, 20),
	}
}

func (c *CronAdapter) Name() string { return c.name }
func (c *CronAdapter) Kind() string { return KindCron }

func (c *CronAdapter) Start(ctx context.Context) (<-chan Event, error) {
	out := make(chan Event, 20)
	c.stopCh = make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopCh:
				return
			case event, ok := <-c.eventsCh:
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

func (c *CronAdapter) Stop() {
	if c.stopCh != nil {
		select {
		case <-c.stopCh:
		default:
			close(c.stopCh)
		}
	}
}

// EmitJobFired is called when a cron job executes.
func (c *CronAdapter) EmitJobFired(jobID, jobName, channel, chatID string) {
	event := NewEvent(KindCron, "cron", PriorityNormal).
		WithChannel(channel, chatID).
		WithPayload("job_id", jobID).
		WithPayload("job_name", jobName)
	select {
	case c.eventsCh <- event:
	default:
	}
}

// DeviceAdapter wraps device hotplug events.
type DeviceAdapter struct {
	name     string
	eventsCh chan Event
	stopCh   chan struct{}
}

// NewDeviceAdapter creates an adapter for device connect/disconnect events.
func NewDeviceAdapter() *DeviceAdapter {
	return &DeviceAdapter{
		name:     "devices",
		eventsCh: make(chan Event, 10),
	}
}

func (d *DeviceAdapter) Name() string { return d.name }
func (d *DeviceAdapter) Kind() string { return KindDevice }

func (d *DeviceAdapter) Start(ctx context.Context) (<-chan Event, error) {
	out := make(chan Event, 10)
	d.stopCh = make(chan struct{})
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case <-d.stopCh:
				return
			case event, ok := <-d.eventsCh:
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

func (d *DeviceAdapter) Stop() {
	if d.stopCh != nil {
		select {
		case <-d.stopCh:
		default:
			close(d.stopCh)
		}
	}
}

// EmitDeviceEvent is called when a device is connected or disconnected.
func (d *DeviceAdapter) EmitDeviceEvent(action, deviceKind, deviceName, channel, chatID string) {
	event := NewEvent(KindDevice, "devices", PriorityHigh).
		WithChannel(channel, chatID).
		WithPayload("action", action).
		WithPayload("device_kind", deviceKind).
		WithPayload("device_name", deviceName)
	select {
	case d.eventsCh <- event:
	default:
	}
}

// License: MIT

package channels

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/constants"
	"github.com/Vatthu/vikram/pkg/logger"
)

type Manager struct {
	channels     map[string]Channel
	bus          *bus.MessageBus
	config       *config.Config
	dispatchTask *asyncTask
	mu           sync.RWMutex
}

type asyncTask struct {
	cancel context.CancelFunc
}

func NewManager(cfg *config.Config, messageBus *bus.MessageBus) (*Manager, error) {
	m := &Manager{
		channels: make(map[string]Channel),
		bus:      messageBus,
		config:   cfg,
	}

	if err := m.initChannels(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) initChannels() error {
	logger.InfoC("channels", "Initializing channel manager")

	if m.config.Channels.Telegram.Enabled && m.config.Channels.Telegram.Token != "" {
		logger.DebugC("channels", "Attempting to initialize Telegram channel")
		telegram, err := NewTelegramChannel(m.config, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize Telegram channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["telegram"] = telegram
			logger.InfoC("channels", "Telegram channel enabled successfully")
		}
	}

	if m.config.Channels.WhatsApp.Enabled && m.config.Channels.WhatsApp.BridgeURL != "" {
		logger.DebugC("channels", "Attempting to initialize WhatsApp channel")
		whatsapp, err := NewWhatsAppChannel(m.config.Channels.WhatsApp, m.bus)
		if err != nil {
			logger.ErrorCF("channels", "Failed to initialize WhatsApp channel", map[string]interface{}{
				"error": err.Error(),
			})
		} else {
			m.channels["whatsapp"] = whatsapp
			logger.InfoC("channels", "WhatsApp channel enabled successfully")
		}
	}

	logger.InfoCF("channels", "Channel initialization completed", map[string]interface{}{
		"enabled_channels": len(m.channels),
	})

	return nil
}

func (m *Manager) StartAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.channels) == 0 {
		logger.WarnC("channels", "No channels enabled")
		return nil
	}

	logger.InfoC("channels", "Starting all channels")

	dispatchCtx, cancel := context.WithCancel(ctx)
	m.dispatchTask = &asyncTask{cancel: cancel}

	go m.dispatchOutbound(dispatchCtx)

	for name, channel := range m.channels {
		logger.InfoCF("channels", "Starting channel", map[string]interface{}{
			"channel": name,
		})
		if err := channel.Start(ctx); err != nil {
			logger.ErrorCF("channels", "Failed to start channel", map[string]interface{}{
				"channel": name,
				"error":   err.Error(),
			})
		}
	}

	logger.InfoC("channels", "All channels started")
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	logger.InfoC("channels", "Stopping all channels")

	if m.dispatchTask != nil {
		m.dispatchTask.cancel()
		m.dispatchTask = nil
	}

	for name, channel := range m.channels {
		logger.InfoCF("channels", "Stopping channel", map[string]interface{}{
			"channel": name,
		})
		if err := channel.Stop(ctx); err != nil {
			logger.ErrorCF("channels", "Error stopping channel", map[string]interface{}{
				"channel": name,
				"error":   err.Error(),
			})
		}
	}

	logger.InfoC("channels", "All channels stopped")
	return nil
}

func (m *Manager) dispatchOutbound(ctx context.Context) {
	logger.InfoC("channels", "Outbound dispatcher started")

	sub := m.bus.SubscribeOutbound()
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			logger.InfoC("channels", "Outbound dispatcher stopped")
			return
		case msg, ok := <-sub.C:
			if !ok {
				continue
			}

			// Silently skip internal channels
			if constants.IsInternalChannel(msg.Channel) {
				continue
			}

			m.mu.RLock()
			channel, exists := m.channels[msg.Channel]
			m.mu.RUnlock()

			if !exists {
				logger.WarnCF("channels", "Unknown channel for outbound message", map[string]interface{}{
					"channel": msg.Channel,
				})
				continue
			}

			if err := channel.Send(ctx, msg); err != nil {
				logger.ErrorCF("channels", "Error sending message to channel", map[string]interface{}{
					"channel": msg.Channel,
					"error":   err.Error(),
				})
			} else {
				logger.InfoCF("channels", "Outbound message delivered to channel", map[string]interface{}{
					"channel":      msg.Channel,
					"chat_id":      msg.ChatID,
					"content_len":  len(msg.Content),
					"content_head": truncateForChannelLog(msg.Content),
				})
			}
		}
	}
}

func truncateForChannelLog(content string) string {
	const limit = 80
	if len(content) <= limit {
		return content
	}
	return content[:limit] + "..."
}

func (m *Manager) ReconnectChannel(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("channel name is required")
	}

	m.mu.Lock()
	current := m.channels[name]
	delete(m.channels, name)
	m.mu.Unlock()

	if current != nil {
		if err := current.Stop(ctx); err != nil {
			logger.WarnCF("channels", "Error stopping channel before reconnect", map[string]interface{}{
				"channel": name,
				"error":   err.Error(),
			})
		}
	}

	var next Channel
	switch name {
	case "telegram":
		if m.config.Channels.Telegram.Enabled && m.config.Channels.Telegram.Token != "" {
			tg, err := NewTelegramChannel(m.config, m.bus)
			if err != nil {
				return fmt.Errorf("telegram reconnect failed: %w", err)
			}
			next = tg
		}
	case "whatsapp":
		if m.config.Channels.WhatsApp.Enabled && m.config.Channels.WhatsApp.BridgeURL != "" {
			wa, err := NewWhatsAppChannel(m.config.Channels.WhatsApp, m.bus)
			if err != nil {
				return fmt.Errorf("whatsapp reconnect failed: %w", err)
			}
			next = wa
		}
	default:
		return fmt.Errorf("unknown channel %q", name)
	}

	if next == nil {
		logger.InfoCF("channels", "Channel disabled after config update", map[string]interface{}{
			"channel": name,
		})
		return nil
	}

	if err := next.Start(ctx); err != nil {
		return fmt.Errorf("%s start failed: %w", name, err)
	}

	m.mu.Lock()
	m.channels[name] = next
	m.mu.Unlock()
	logger.InfoCF("channels", "Channel reconnected", map[string]interface{}{
		"channel": name,
	})
	return nil
}

func (m *Manager) GetChannel(name string) (Channel, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	channel, ok := m.channels[name]
	return channel, ok
}

func (m *Manager) GetStatus() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := make(map[string]interface{})
	for name, channel := range m.channels {
		status[name] = map[string]interface{}{
			"enabled": true,
			"running": channel.IsRunning(),
		}
	}
	return status
}

func (m *Manager) GetEnabledChannels() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

func (m *Manager) RegisterChannel(name string, channel Channel) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.channels[name] = channel
}

func (m *Manager) UnregisterChannel(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.channels, name)
}

func (m *Manager) SendToChannel(ctx context.Context, channelName, chatID, content string) error {
	m.mu.RLock()
	channel, exists := m.channels[channelName]
	m.mu.RUnlock()

	if !exists {
		return fmt.Errorf("channel %s not found", channelName)
	}

	msg := bus.OutboundMessage{
		Channel: channelName,
		ChatID:  chatID,
		Content: content,
	}

	return channel.Send(ctx, msg)
}

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/utils"
)

type WhatsAppChannel struct {
	*BaseChannel
	conn      *websocket.Conn
	config    config.WhatsAppConfig
	url       string
	mu        sync.Mutex
	connected bool
}

func NewWhatsAppChannel(cfg config.WhatsAppConfig, bus *bus.MessageBus) (*WhatsAppChannel, error) {
	base := NewBaseChannel("whatsapp", cfg, bus, cfg.AllowFrom)

	return &WhatsAppChannel{
		BaseChannel: base,
		config:      cfg,
		url:         cfg.BridgeURL,
		connected:   false,
	}, nil
}

func (c *WhatsAppChannel) Start(ctx context.Context) error {
	logger.InfoC("whatsapp", fmt.Sprintf("Starting WhatsApp channel connecting to %s...", c.url))

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	var header http.Header
	if c.config.BridgeToken != "" {
		header = http.Header{}
		header.Set("Authorization", "Bearer "+c.config.BridgeToken)
	}

	conn, _, err := dialer.Dial(c.url, header)
	if err != nil {
		return fmt.Errorf("failed to connect to WhatsApp bridge: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	c.setRunning(true)
	logger.InfoC("whatsapp", "WhatsApp channel connected")

	go c.listen(ctx)

	return nil
}

func (c *WhatsAppChannel) Stop(ctx context.Context) error {
	logger.InfoC("whatsapp", "Stopping WhatsApp channel...")

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			logger.ErrorC("whatsapp", fmt.Sprintf("Error closing WhatsApp connection: %v", err))
		}
		c.conn = nil
	}

	c.connected = false
	c.setRunning(false)

	return nil
}

func (c *WhatsAppChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("whatsapp connection not established")
	}

	payload := map[string]interface{}{
		"type":    "message",
		"to":      msg.ChatID,
		"content": msg.Content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal message: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

func (c *WhatsAppChannel) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()

			if conn == nil {
				time.Sleep(1 * time.Second)
				continue
			}

			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.WarnC("whatsapp", fmt.Sprintf("WhatsApp read error: %v", err))
				time.Sleep(2 * time.Second)
				continue
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				logger.WarnC("whatsapp", fmt.Sprintf("Failed to unmarshal WhatsApp message: %v", err))
				continue
			}

			msgType, ok := msg["type"].(string)
			if !ok {
				continue
			}

			if msgType == "message" {
				c.handleIncomingMessage(msg)
			}
		}
	}
}

func (c *WhatsAppChannel) handleIncomingMessage(msg map[string]interface{}) {
	senderID, ok := msg["from"].(string)
	if !ok {
		return
	}

	chatID, ok := msg["chat"].(string)
	if !ok {
		chatID = senderID
	}

	content, ok := msg["content"].(string)
	if !ok {
		content = ""
	}

	var mediaPaths []string
	if mediaData, ok := msg["media"].([]interface{}); ok {
		mediaPaths = make([]string, 0, len(mediaData))
		for _, m := range mediaData {
			if path, ok := m.(string); ok {
				mediaPaths = append(mediaPaths, path)
			}
		}
	}

	metadata := make(map[string]string)
	if messageID, ok := msg["id"].(string); ok {
		metadata["message_id"] = messageID
	}
	if userName, ok := msg["from_name"].(string); ok {
		metadata["user_name"] = userName
	}

	logger.DebugC("whatsapp", fmt.Sprintf("WhatsApp message from %s: %s...", senderID, utils.Truncate(content, 50)))

	c.HandleMessage(senderID, chatID, content, mediaPaths, metadata)
}

package tools

import (
	"context"
	"fmt"
)

// MessageTool sends messages directly to the user or a specific channel/chat.
// It is intended for the agent to communicate directly or provide updates.
type MessageTool struct {
	sendCallback func(channel, chatID, content string) error
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

// SetSendCallback allows the agent loop to inject its outbound message publishing.
func (t *MessageTool) SetSendCallback(callback func(channel, chatID, content string) error) {
	t.sendCallback = callback
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a message to the user or a specific channel/chat. Use this tool only for direct communication, not for tool results."
}

func (t *MessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The message content to send.",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional. The target channel (e.g., 'discord', 'telegram'). Defaults to the current inbound channel if not specified.",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional. The target chat ID. Defaults to the current inbound chat ID if not specified.",
			},
		},
		"required": []string{"content"},
	}
}

// Execute sends a message using the provided ToolContext.
func (t *MessageTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("content is required")
	}

	targetChannel := tc.Channel
	if ch, ok := args["channel"].(string); ok && ch != "" {
		targetChannel = ch
	}
	targetChatID := tc.ChatID
	if cid, ok := args["chat_id"].(string); ok && cid != "" {
		targetChatID = cid
	}

	if t.sendCallback == nil {
		return ErrorResult("message send callback not configured")
	}

	if targetChannel == "" || targetChatID == "" {
		return ErrorResult("No target channel/chat specified")
	}

	if err := t.sendCallback(targetChannel, targetChatID, content); err != nil {
		return ErrorResult(fmt.Sprintf("failed to send message: %v", err))
	}

	return SilentResult("Message sent.")
}

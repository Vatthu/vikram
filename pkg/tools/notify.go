package tools

import (
	"context"
	"fmt"

	"github.com/v1claw/levik/pkg/bus"
)

// NotifyUserTool sends a project completion summary to the user's active channel.
// The brain calls this after ALL delegated work is done.
type NotifyUserTool struct{}

// NewNotifyUserTool creates a new NotifyUserTool.
func NewNotifyUserTool() *NotifyUserTool { return &NotifyUserTool{} }

func (t *NotifyUserTool) Name() string { return "notify_user" }

func (t *NotifyUserTool) Description() string {
	return "Notify the user that a project or multi-step task is fully complete. " +
		"Sends a summary message to their active channel (Telegram, Discord, CLI, etc.) " +
		"and optionally asks for feedback. Use this after ALL delegated work is done."
}

func (t *NotifyUserTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"message": map[string]interface{}{
				"type":        "string",
				"description": "Summary of completed work to send to the user.",
			},
			"request_feedback": map[string]interface{}{
				"type":        "boolean",
				"description": "If true, append a prompt asking for feedback/review.",
			},
		},
		"required": []string{"message"},
	}
}

func (t *NotifyUserTool) Execute(_ context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	message, _ := args["message"].(string)
	if message == "" {
		return ErrorResult("message is required")
	}

	requestFeedback, _ := args["request_feedback"].(bool)
	content := message
	if requestFeedback {
		content += "\n\nPlease review and share your feedback — what should I change or improve?"
	}

	if tc.Bus == nil {
		return ErrorResult("message bus not available")
	}

	channel := tc.Channel
	chatID := tc.ChatID
	if channel == "" || chatID == "" {
		return ErrorResult("no active channel to notify")
	}

	tc.Bus.PublishOutbound(bus.OutboundMessage{
		Channel: channel,
		ChatID:  chatID,
		Content: fmt.Sprintf("🎉 %s", content),
	})

	return SilentResult(fmt.Sprintf("User notified on %s:%s", channel, chatID))
}

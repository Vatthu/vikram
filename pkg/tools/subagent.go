package tools

import (
	"context"
	"fmt"
)

// SubagentTool allows the agent to create and run a subagent directly.
// Unlike `spawn`, this is a blocking call and the subagent runs synchronously.
type SubagentTool struct {
	manager *SubagentManager
}

func NewSubagentTool(manager *SubagentManager) *SubagentTool {
	return &SubagentTool{manager: manager}
}

func (t *SubagentTool) Name() string {
	return "subagent"
}

func (t *SubagentTool) Description() string {
	return "Create and run a subagent to work on a specific task. This call blocks until the subagent completes its task."
}

func (t *SubagentTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The detailed task description for the subagent.",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "A short, unique label for this subagent instance (e.g., 'code_reviewer', 'bug_fixer').",
			},
		},
		"required": []string{"task", "label"},
	}
}

func (t *SubagentTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	task, ok := args["task"].(string)
	if !ok {
		return ErrorResult("task is required")
	}
	label, ok := args["label"].(string)
	if !ok {
		return ErrorResult("label is required")
	}

	if t.manager == nil {
		return ErrorResult("subagent manager is not configured")
	}

	// This is a synchronous call, so no asyncCallback is directly passed.
	// The result is returned directly.
	loopResult, err := t.manager.RunToolLoop(ctx, task, label, tc.Channel, tc.ChatID, tc.SessionKey)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}

	labelStr := label
	if labelStr == "" {
		labelStr = "subagent"
	}

	userContent := fmt.Sprintf("Subagent '%s' completed task in %d iterations.\nResult:\n%s",
		labelStr, loopResult.Iterations, loopResult.Content)
	llmContent := userContent

	return &ToolResult{
		ForLLM:  llmContent,
		ForUser: userContent,
		Silent:  false,
		IsError: false,
		Async:   false,
	}
}

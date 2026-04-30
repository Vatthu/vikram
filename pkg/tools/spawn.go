package tools

import (
	"context"
)

type SpawnTool struct {
	manager *SubagentManager
}

func NewSpawnTool(manager *SubagentManager) *SpawnTool {
	return &SpawnTool{manager: manager}
}

func (t *SpawnTool) Name() string {
	return "spawn"
}

func (t *SpawnTool) Description() string {
	return "Spawn a subagent to work on a specific task in the background. The subagent will run for a limited number of iterations and report its final answer via the message bus."
}

func (t *SpawnTool) Parameters() map[string]interface{} {
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

func (t *SpawnTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
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

	// Pass the original ToolContext to Spawn
	msg, err := t.manager.Spawn(ctx, task, label, tc)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}

	// Return AsyncResult since the task runs in background
	return AsyncResult(msg)
}

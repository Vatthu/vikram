package agent

import (
	"context"

	"github.com/v1claw/levik/pkg/proactive/sop"
	"github.com/v1claw/levik/pkg/tools"
)

type SOPTool struct {
	orchestrator *sop.Orchestrator
}

func NewSOPTool(orchestrator *sop.Orchestrator) *SOPTool {
	return &SOPTool{
		orchestrator: orchestrator,
	}
}

func (t *SOPTool) Name() string {
	return "start_sop"
}

func (t *SOPTool) Description() string {
	return "Starts the Standard Operating Procedure (SOP) loop for a complex engineering task. It delegates the task to a sequence of subagents (Lead -> Coder -> QA -> Reviewer) for autonomous execution."
}

func (t *SOPTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The complex engineering task to be handled by the SOP loop.",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SOPTool) Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult {
	task, ok := args["task"].(string)
	if !ok {
		return tools.ErrorResult("task string is required")
	}

	// SOP runs asynchronously so the main agent isn't blocked completely
	go func() {
		err := t.orchestrator.Run(context.Background(), task, tc.Channel, tc.ChatID, tc.SessionKey)
		if err != nil {
			if tc.Async != nil {
				if tc.AsyncCtx != nil {
					tc.Async(tc.AsyncCtx, tools.ErrorResult(err.Error()))
				} else {
					tc.Async(context.Background(), tools.ErrorResult(err.Error()))
				}
			}
			return
		}
		if tc.Async != nil {
			if tc.AsyncCtx != nil {
				tc.Async(tc.AsyncCtx, tools.NewToolResult("SOP loop executed successfully."))
			} else {
				tc.Async(context.Background(), tools.NewToolResult("SOP loop executed successfully."))
			}
		}
	}()

	return tools.AsyncResult("SOP Loop has started in the background. You will be notified with the results upon completion.")
}

package tools

import (
	"context"
	"fmt"
	"strings"
)

// DelegateTaskTool allows the brain to assign work to the most appropriate
// specialized CLI worker. The brain chooses the worker autonomously based on
// the task — it should never ask the user which worker to use.
type DelegateTaskTool struct {
	manager *SubagentManager
}

// workerSpecialties describes what each known CLI worker is best at.
// The brain uses this context to pick the right tool without user guidance.
var workerSpecialties = map[string]string{
	"standard": "general tasks handled by the primary brain's own subagent",
	"lead":     "heaviest lifting: architecture, integration, complex decisions, working engineer",
	"engineer": "implementation work: writing code, refactoring, file creation",
	"reviewer": "independent code review: finds bugs and issues in code written by others",
	"runner":   "test execution, verification, small defined tasks, diff-checking",
	"gemini":   "research, analysis, multi-modal reasoning, brainstorming (CLI worker)",
	"codex":    "code writing, implementation, refactoring, file creation (CLI worker)",
	"claude":   "long-context reasoning, writing, nuanced analysis (CLI worker)",
}

// NewDelegateTaskTool creates a new delegation tool.
func NewDelegateTaskTool(manager *SubagentManager) *DelegateTaskTool {
	return &DelegateTaskTool{manager: manager}
}

func (t *DelegateTaskTool) Name() string {
	return "delegate_task"
}

func (t *DelegateTaskTool) Description() string {
	return "Delegate a task to the most appropriate specialized worker. " +
		"YOU choose which worker fits the task best — do not ask the user. " +
		"For multi-component projects, call this tool once per component using the best available worker for each. " +
		"The call blocks until the worker finishes and returns its result for you to review before continuing."
}

func (t *DelegateTaskTool) Parameters() map[string]interface{} {
	// Build worker list and per-worker specialization hints for the brain.
	// Include registered team agents (by role) and any CLI workers.
	workerList := []string{"standard"}
	var specialtyLines []string
	specialtyLines = append(specialtyLines, fmt.Sprintf("  standard — %s", workerSpecialties["standard"]))

	if t.manager != nil {
		for _, role := range t.manager.RegisteredRoles() {
			workerList = append(workerList, role)
			if spec, ok := workerSpecialties[role]; ok {
				specialtyLines = append(specialtyLines, fmt.Sprintf("  %s — %s", role, spec))
			} else {
				specialtyLines = append(specialtyLines, fmt.Sprintf("  %s — team agent", role))
			}
		}
		for name := range t.manager.CLIProviders() {
			workerList = append(workerList, name)
			if spec, ok := workerSpecialties[name]; ok {
				specialtyLines = append(specialtyLines, fmt.Sprintf("  %s — %s", name, spec))
			} else {
				specialtyLines = append(specialtyLines, fmt.Sprintf("  %s — specialized CLI worker", name))
			}
		}
	}

	workerDesc := "Which worker to assign this task to. Choose the best fit:\n" +
		strings.Join(specialtyLines, "\n")

	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"worker_type": map[string]interface{}{
				"type":        "string",
				"enum":        workerList,
				"description": workerDesc,
			},
			"task": map[string]interface{}{
				"type":        "string",
				"description": "Complete, self-contained instruction for the worker. Include all context it needs — it has no memory of prior conversation.",
			},
		},
		"required": []string{"worker_type", "task"},
	}
}

func (t *DelegateTaskTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	workerType, ok := args["worker_type"].(string)
	if !ok {
		return ErrorResult("worker_type is required")
	}
	task, ok := args["task"].(string)
	if !ok {
		return ErrorResult("task is required")
	}

	if t.manager == nil {
		return ErrorResult("subagent manager is not configured")
	}

	// Route through Spawn — resolveAgent maps the workerType to the right
	// provider/model/context (by agent ID, then role, then fallback to defaults).
	msg, err := t.manager.Spawn(ctx, task, workerType, tc)
	if err != nil {
		return ErrorResult(err.Error()).WithError(err)
	}
	return AsyncResult(msg)
}

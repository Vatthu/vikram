package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/v1claw/levik/pkg/epistemology"
)

// AssertFactTool allows the agent to store explicit logical facts.
type AssertFactTool struct {
	store epistemology.GraphStore
}

func NewAssertFactTool(store epistemology.GraphStore) *AssertFactTool {
	return &AssertFactTool{store: store}
}

func (t *AssertFactTool) Name() string {
	return "assert_fact"
}

func (t *AssertFactTool) Description() string {
	return "Store a confirmed, logical fact about the user, system, or project state using a Subject-Predicate-Object structure."
}

func (t *AssertFactTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "The entity the fact is about (e.g., 'User', 'CodexAgent', 'MemorySystem').",
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "The relationship or action (e.g., 'prefers', 'is_assigned_to', 'failed_to').",
			},
			"object": map[string]interface{}{
				"type":        "string",
				"description": "The target or value of the relationship (e.g., 'concise answers', 'task_123', 'compile_error').",
			},
			"confidence": map[string]interface{}{
				"type":        "number",
				"description": "How certain you are about this fact (0.0 to 1.0).",
			},
		},
		"required": []string{"subject", "predicate", "object", "confidence"},
	}
}

func (t *AssertFactTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	subject, _ := args["subject"].(string)
	predicate, _ := args["predicate"].(string)
	object, _ := args["object"].(string)
	confidence, _ := args["confidence"].(float64)

	id, err := t.store.AssertFact(subject, predicate, object, "llm_inference", confidence)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to assert fact: %v", err)).WithError(err)
	}

	return NewToolResult(fmt.Sprintf("Fact successfully stored with ID: %s", id))
}

// QueryGraphTool allows the agent to recall exact facts.
type QueryGraphTool struct {
	store epistemology.GraphStore
}

func NewQueryGraphTool(store epistemology.GraphStore) *QueryGraphTool {
	return &QueryGraphTool{store: store}
}

func (t *QueryGraphTool) Name() string {
	return "query_knowledge_graph"
}

func (t *QueryGraphTool) Description() string {
	return "Query the structured memory graph for exact, confirmed facts."
}

func (t *QueryGraphTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Optional. The entity to search for.",
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Optional. The relationship to search for.",
			},
			"object": map[string]interface{}{
				"type":        "string",
				"description": "Optional. A keyword in the object field.",
			},
			"min_confidence": map[string]interface{}{
				"type":        "number",
				"description": "Optional. Minimum confidence score (0.0 to 1.0).",
			},
		},
	}
}

func (t *QueryGraphTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	var queryArgs epistemology.Query

	if val, ok := args["subject"].(string); ok {
		queryArgs.Subject = val
	}
	if val, ok := args["predicate"].(string); ok {
		queryArgs.Predicate = val
	}
	if val, ok := args["object"].(string); ok {
		queryArgs.Object = val
	}
	if val, ok := args["min_confidence"].(float64); ok {
		queryArgs.MinConf = val
	}

	facts, err := t.store.Query(queryArgs)
	if err != nil {
		return ErrorResult(fmt.Sprintf("query failed: %v", err)).WithError(err)
	}

	if len(facts) == 0 {
		return NewToolResult("No facts matched your query.")
	}

	resultJSON, _ := json.MarshalIndent(facts, "", "  ")
	return NewToolResult(fmt.Sprintf("Found %d matching facts:\n%s", len(facts), string(resultJSON)))
}

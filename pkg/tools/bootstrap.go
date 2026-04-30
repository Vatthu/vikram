package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type BootstrapTool struct {
	workspace string
}

func NewBootstrapTool(workspace string) *BootstrapTool {
	return &BootstrapTool{workspace: workspace}
}

func (t *BootstrapTool) Name() string {
	return "complete_bootstrap"
}

func (t *BootstrapTool) Description() string {
	return "Finalize first-run bootstrap by writing the assistant identity, soul, user profile, memory, and removing BOOTSTRAP.md once enough information is known."
}

func (t *BootstrapTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"assistant_name": map[string]interface{}{
				"type":        "string",
				"description": "The final name the assistant should use.",
			},
			"user_name": map[string]interface{}{
				"type":        "string",
				"description": "The operator's preferred name.",
			},
			"relationship": map[string]interface{}{
				"type":        "string",
				"description": "Who the assistant should be to the operator.",
			},
			"role": map[string]interface{}{
				"type":        "string",
				"description": "The assistant's core role or purpose.",
			},
			"tone": map[string]interface{}{
				"type":        "string",
				"description": "The desired tone and communication style.",
			},
			"priorities": map[string]interface{}{
				"type":        "string",
				"description": "The assistant's priorities and working style.",
			},
			"boundaries": map[string]interface{}{
				"type":        "string",
				"description": "Important boundaries or constraints to follow.",
			},
			"user_preferences": map[string]interface{}{
				"type":        "string",
				"description": "User preferences the assistant should remember.",
			},
		},
		"required": []string{"assistant_name", "user_name", "relationship", "role", "tone", "priorities"},
	}
}

func (t *BootstrapTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	_ = ctx
	_ = tc
	assistantName, ok := stringArg(args, "assistant_name")
	if !ok {
		return ErrorResult("assistant_name is required")
	}
	userName, ok := stringArg(args, "user_name")
	if !ok {
		return ErrorResult("user_name is required")
	}
	relationship, ok := stringArg(args, "relationship")
	if !ok {
		return ErrorResult("relationship is required")
	}
	role, ok := stringArg(args, "role")
	if !ok {
		return ErrorResult("role is required")
	}
	tone, ok := stringArg(args, "tone")
	if !ok {
		return ErrorResult("tone is required")
	}
	priorities, ok := stringArg(args, "priorities")
	if !ok {
		return ErrorResult("priorities is required")
	}
	boundaries, _ := stringArg(args, "boundaries")
	userPreferences, _ := stringArg(args, "user_preferences")

	if strings.TrimSpace(t.workspace) == "" {
		return ErrorResult("workspace is not configured")
	}

	if err := os.MkdirAll(filepath.Join(t.workspace, "memory"), 0700); err != nil {
		return ErrorResult(fmt.Sprintf("failed to prepare workspace: %v", err))
	}

	files := map[string]string{
		"AGENT.md": fmt.Sprintf(`# Agent Instructions

You are %s.

## Role
%s

## Relationship
%s

## Operating Rules

- Act like a present, awake assistant for %s.
- Keep replies grounded, direct, and useful.
- Follow the personality in SOUL.md and the operator profile in USER.md.
- Use tools for real actions; do not pretend something was done.
- When the operator asks who you are, answer briefly from role and relationship first.
- Do not dump a capability list unless the operator explicitly asks what you can do.
`, assistantName, role, relationship, userName),
		"IDENTITY.md": fmt.Sprintf(`# Identity

## Name
%s

## Role
%s

## Relationship
%s

## How to Speak
%s
- Keep introductions short and natural.
- Do not default to README-style summaries, product blurbs, or capability dumps.
`, assistantName, role, relationship, bulletize(tone)),
		"SOUL.md": fmt.Sprintf(`# Soul

## Tone
%s

## Priorities
%s

## Boundaries
%s
- No marketing voice, no chest-beating, no unnecessary self-description.
`, bulletize(tone), bulletize(priorities), bulletize(defaultIfEmpty(boundaries, "Respect the operator's trust and be explicit about limits, failures, and risks."))),
		"USER.md": fmt.Sprintf(`# User

## Primary Operator
- Name: %s

## Preferences
%s
`, userName, bulletize(defaultIfEmpty(userPreferences, priorities))),
		"TOOLS.md": `# Tools

## Guidance

- Use tools to do real work; do not claim an action happened unless a tool actually completed it.
- Prefer the smallest safe action that solves the operator's request.
- If a tool fails, say what failed and what you will try next.
- Keep file and shell work grounded in the current workspace unless the operator explicitly wants broader access.
`,
		"memory/MEMORY.md": fmt.Sprintf(`# Long-term Memory

This file stores important information that should persist across sessions.

## Core Identity (Soul)
- Name: %s
- Core Purpose: %s

## User Information
- Name: %s

## Preferences
%s

## Important Notes
- Bootstrap completed successfully.
`, assistantName, role, userName, bulletize(defaultIfEmpty(userPreferences, priorities))),
	}

	for relPath, content := range files {
		targetPath := filepath.Join(t.workspace, relPath)
		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return ErrorResult(fmt.Sprintf("failed to create directory for %s: %v", relPath, err))
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(relPath, "MEMORY.md") {
			mode = 0600
		}
		if err := os.WriteFile(targetPath, []byte(content), mode); err != nil {
			return ErrorResult(fmt.Sprintf("failed to write %s: %v", relPath, err))
		}
	}

	bootstrapPath := filepath.Join(t.workspace, "BOOTSTRAP.md")
	if err := os.Remove(bootstrapPath); err != nil && !os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("failed to remove BOOTSTRAP.md: %v", err))
	}

	return SilentResult(fmt.Sprintf("Bootstrap completed for %s. Identity persisted and BOOTSTRAP.md removed.", assistantName))
}

func stringArg(args map[string]interface{}, key string) (string, bool) {
	value, ok := args[key].(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func bulletize(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	lines := strings.Split(raw, "\n")
	var parts []string
	for _, line := range lines {
		line = strings.TrimSpace(strings.TrimPrefix(line, "-"))
		if line == "" {
			continue
		}
		parts = append(parts, "- "+line)
	}
	if len(parts) == 0 {
		return "- " + raw
	}
	return strings.Join(parts, "\n")
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

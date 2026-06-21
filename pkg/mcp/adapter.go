package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/Vatthu/vikram/pkg/tools"
)

// Adapter wraps an MCP tool as a Vikram Tool implementation.
type Adapter struct {
	client    *Client
	toolDef   ToolDef
	prefix    string
	maxOutput int
}

// NewAdapter creates a Vikram Tool from an MCP tool definition.
func NewAdapter(client *Client, def ToolDef, prefix string, maxOutput int) *Adapter {
	if maxOutput == 0 {
		maxOutput = 50000
	}
	return &Adapter{
		client:    client,
		toolDef:   def,
		prefix:    prefix,
		maxOutput: maxOutput,
	}
}

func (a *Adapter) Name() string {
	return a.prefix + sanitizeName(a.toolDef.Name)
}

func (a *Adapter) Description() string {
	return fmt.Sprintf("[MCP] %s", a.toolDef.Description)
}

func (a *Adapter) Parameters() map[string]interface{} {
	return a.toolDef.InputSchema
}

func (a *Adapter) Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult {
	result, err := a.client.CallTool(ctx, a.toolDef.Name, args)
	if err != nil {
		return tools.ErrorResult(fmt.Sprintf("MCP tool %s failed: %v", a.toolDef.Name, err))
	}

	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, fmt.Sprintf("[image: %s, %d bytes]", block.MimeType, len(block.Data)))
		case "resource":
			parts = append(parts, fmt.Sprintf("[resource: %s]", block.MimeType))
		}
	}
	output := strings.Join(parts, "\n")

	if len(output) > a.maxOutput {
		output = output[:a.maxOutput] + fmt.Sprintf("\n\n[MCP output truncated: %d total chars]", len(output))
	}

	return &tools.ToolResult{
		ForLLM:  output,
		IsError: result.IsError,
	}
}

func sanitizeName(name string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
}

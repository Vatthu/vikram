package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/v1claw/levik/pkg/bus"
	"github.com/v1claw/levik/pkg/logger"
	"github.com/v1claw/levik/pkg/providers"
)

// AsyncCallback is a function that a tool can call to send an asynchronous result back to the agent.
type AsyncCallback func(ctx context.Context, result *ToolResult)

// ToolContext provides invocation-specific context to a tool execution.
// This replaces mutable fields on tool instances to prevent cross-session data races.
type ToolContext struct {
	Channel    string
	ChatID     string
	SessionKey string
	SenderID   string
	Async      AsyncCallback
	Bus        *bus.MessageBus
	// AsyncCtx is the agent's root context, propagated to async tool goroutines so
	// they are cancelled cleanly when Stop() is called.  Falls back to
	// context.Background() when nil for backwards-compatible call sites.
	AsyncCtx context.Context
}

// Tool is the interface that all tools must implement.
type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]interface{}
	Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult
}

// ToolRegistry manages the lifecycle and execution of Tools.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new, empty ToolRegistry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a Tool to the registry.
func (tr *ToolRegistry) Register(tool Tool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.tools[tool.Name()] = tool
	logger.DebugC("tools", fmt.Sprintf("Registered tool: %s", tool.Name()))
}

// Get retrieves a Tool by name.
func (tr *ToolRegistry) Get(name string) (Tool, bool) {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	tool, ok := tr.tools[name]
	return tool, ok
}

// List returns a list of all registered tool names.
func (tr *ToolRegistry) List() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	names := make([]string, 0, len(tr.tools))
	for name := range tr.tools {
		names = append(names, name)
	}
	return names
}

// GetSummaries returns a list of formatted strings summarizing each tool.
func (tr *ToolRegistry) GetSummaries() []string {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	var summaries []string
	for _, tool := range tr.tools {
		summaries = append(summaries, fmt.Sprintf("- **%s**: %s", tool.Name(), tool.Description()))
	}
	return summaries
}

// ToProviderDefs converts all registered tools into a format suitable for an LLM provider.
func (tr *ToolRegistry) ToProviderDefs() []providers.ToolDefinition {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	defs := make([]providers.ToolDefinition, 0, len(tr.tools))
	for _, tool := range tr.tools {
		// Use Tool.Parameters() method directly
		defs = append(defs, providers.ToolDefinition{
			Type: "function",
			Function: providers.ToolFunctionDefinition{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  tool.Parameters(),
			},
		})
	}
	return defs
}

// ExecuteWithContext executes a tool with the provided context and arguments.
func (tr *ToolRegistry) ExecuteWithContext(ctx context.Context, toolName string, args map[string]interface{}, tc ToolContext) *ToolResult {
	tool, ok := tr.Get(toolName)
	if !ok {
		return ErrorResult(fmt.Sprintf("Tool %s not found", toolName))
	}

	logger.DebugCF("tools", fmt.Sprintf("Executing tool %s", toolName),
		map[string]interface{}{"args": args})

	result := tool.Execute(ctx, tc, args)

	if result.IsError {
		logger.ErrorCF("tools", fmt.Sprintf("Tool %s failed", toolName),
			map[string]interface{}{"error": result.ForLLM})
	} else {
		logger.DebugCF("tools", fmt.Sprintf("Tool %s executed successfully", toolName),
			map[string]interface{}{"result_len": len(result.ForLLM)})
	}
	return result
}

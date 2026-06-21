package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/providers"
	"github.com/Vatthu/vikram/pkg/utils"
)

// ToolLoopConfig configures the tool execution loop.
type ToolLoopConfig struct {
	Provider      providers.LLMProvider
	Model         string
	Tools         *ToolRegistry
	MaxIterations int
	LLMOptions    map[string]any
}

// ToolLoopResult contains the result of running the tool loop.
type ToolLoopResult struct {
	Content    string
	Iterations int
}

// RunToolLoop executes the LLM + tool call iteration loop.
// This is the core agent logic that can be reused by both main agent and subagents.
func RunToolLoop(ctx context.Context, config ToolLoopConfig, messages []providers.Message, tc ToolContext) (*ToolLoopResult, error) { // Updated to accept ToolContext
	iteration := 0
	var finalContent string

	for iteration < config.MaxIterations {
		iteration++

		logger.DebugCF("toolloop", "LLM iteration",
			map[string]any{
				"iteration": iteration,
				"max":       config.MaxIterations,
			})

		// 1. Build tool definitions
		var providerToolDefs []providers.ToolDefinition
		if config.Tools != nil {
			providerToolDefs = config.Tools.ToProviderDefs()
		}

		// 2. Set default LLM options
		llmOpts := config.LLMOptions
		if llmOpts == nil {
			llmOpts = map[string]any{
				"max_tokens":  4096,
				"temperature": 0.7,
			}
		}

		// 3. Call LLM
		response, err := config.Provider.Chat(ctx, messages, providerToolDefs, config.Model, llmOpts)
		if err != nil {
			logger.ErrorCF("toolloop", "LLM call failed",
				map[string]any{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return nil, fmt.Errorf("LLM call failed: %w", err)
		}

		// 4. If no tool calls, we're done
		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("toolloop", "LLM response without tool calls (direct answer)",
				map[string]any{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		// 5. Log tool calls
		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("toolloop", "LLM requested tool calls",
			map[string]any{
				"tools":     toolNames,
				"count":     len(response.ToolCalls),
				"iteration": iteration,
			})

		// 6. Build assistant message with tool calls
		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}
		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		// 7. Execute tool calls
		for _, toolCall := range response.ToolCalls { // Renamed 'tc' to 'toolCall'
			argsJSON, _ := json.Marshal(toolCall.Arguments)
			argsPreview := utils.Truncate(string(argsJSON), 200)
			logger.InfoCF("toolloop", fmt.Sprintf("Tool call: %s(%s)", toolCall.Name, argsPreview),
				map[string]any{
					"tool":      toolCall.Name,
					"iteration": iteration,
				})

			// Execute tool with ToolContext.
			// Create a new ToolContext for this specific tool execution from the parent ToolContext.
			toolExecutionTC := ToolContext{
				Channel:    tc.Channel,
				ChatID:     tc.ChatID,
				SessionKey: tc.SessionKey,
				SenderID:   tc.SenderID,
				Async:      tc.Async, // Pass the async callback if available
				Bus:        tc.Bus,
			}

			var toolResult *ToolResult
			if config.Tools != nil {
				toolResult = config.Tools.ExecuteWithContext(ctx, toolCall.Name, toolCall.Arguments, toolExecutionTC)
			} else {
				toolResult = ErrorResult("No tools available")
			}

			// Determine content for LLM
			contentForLLM := toolResult.ForLLM
			if contentForLLM == "" && toolResult.Err != nil {
				contentForLLM = toolResult.Err.Error()
			}

			// Add tool result message
			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: toolCall.ID,
			}
			messages = append(messages, toolResultMsg)
		}
	}

	return &ToolLoopResult{
		Content:    finalContent,
		Iterations: iteration,
	}, nil
}

// SubagentManager manages the lifecycle and execution of subagents.
// It also holds a reference to the main LLM provider for subagent instantiation.
type SubagentManager struct {
	provider     providers.LLMProvider
	model        string
	workspace    string
	bus          *bus.MessageBus
	tools        *ToolRegistry
	mu           sync.RWMutex
	cliProviders map[string]providers.LLMProvider // Registered CLI workers
	// msgBuilder is the injected real ContextBuilder.BuildMessages function.
	// When nil, the local stub is used — subagents get bare prompts with no identity/memory.
	// Inject via SetMessageBuilder to break the tools→agent import cycle.
	msgBuilder MessageBuilderFn

	agentEntries map[string]*AgentEntry // agentID to per-agent resources
	roleToID     map[string]string      // role to agentID for dispatch by role
}

// AgentEntry holds the provider, model, message builder, and system prompt
// for a single registered team member.
type AgentEntry struct {
	Provider     providers.LLMProvider
	Model        string
	MsgBuilder   MessageBuilderFn
	SystemPrompt string
}

// MessageBuilderFn is the function signature for building initial subagent messages.
// Matches (*agent.ContextBuilder).BuildMessages, enabling injection without import cycles.
type MessageBuilderFn func(history []providers.Message, summary, userMessage string, media []string, channel, chatID string) []providers.Message

// NewSubagentManager creates a new SubagentManager.
func NewSubagentManager(provider providers.LLMProvider, model, workspace string, msgBus *bus.MessageBus, cliProviders map[string]providers.LLMProvider) *SubagentManager {
	return &SubagentManager{
		provider:     provider,
		model:        model,
		workspace:    workspace,
		bus:          msgBus,
		cliProviders: cliProviders,
		agentEntries: make(map[string]*AgentEntry),
		roleToID:     make(map[string]string),
	}
}

// SetTools sets the tool registry for subagents.
func (sm *SubagentManager) SetTools(tools *ToolRegistry) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.tools = tools
}

// SetMessageBuilder injects the real context-builder function from the agent layer.
// Once set, subagents receive a fully hydrated system prompt (identity, memory, skills).
func (sm *SubagentManager) SetMessageBuilder(fn MessageBuilderFn) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.msgBuilder = fn
}

// RegisterAgent adds a team member so that Spawn and RunToolLoop can route
// to a specific provider, model, and message builder when the label matches
// the agent ID or role.  When role is non-empty the agent also becomes
// addressable by role (used by the SOP orchestrator).
func (sm *SubagentManager) RegisterAgent(agentID, role string, provider providers.LLMProvider, model string, msgBuilder MessageBuilderFn, systemPrompt string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.agentEntries[agentID] = &AgentEntry{
		Provider:     provider,
		Model:        model,
		MsgBuilder:   msgBuilder,
		SystemPrompt: systemPrompt,
	}
	if role != "" {
		sm.roleToID[role] = agentID
	}
}

// UnregisterAgent removes a team member by ID. Returns false if not found.
func (sm *SubagentManager) UnregisterAgent(agentID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	entry, ok := sm.agentEntries[agentID]
	if !ok {
		return false
	}
	// Clean up role mapping if this agent was the one mapped to that role.
	for role, id := range sm.roleToID {
		if id == agentID {
			delete(sm.roleToID, role)
		}
	}
	delete(sm.agentEntries, agentID)
	_ = entry
	return true
}

// ResolveAgent looks up a label (agent ID or role) and returns the
// matching provider, model, message builder, and system prompt.
// Falls back to the default provider/model/message builder when no match is found.
func (sm *SubagentManager) ResolveAgent(label string) (providers.LLMProvider, string, MessageBuilderFn, string) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Exact ID match
	if entry, ok := sm.agentEntries[label]; ok {
		return entry.Provider, entry.Model, entry.MsgBuilder, entry.SystemPrompt
	}
	// Role match
	if id, ok := sm.roleToID[label]; ok {
		if entry, ok := sm.agentEntries[id]; ok {
			return entry.Provider, entry.Model, entry.MsgBuilder, entry.SystemPrompt
		}
	}
	// Fallback to defaults
	return sm.provider, sm.model, sm.msgBuilder, ""
}

// Spawn starts a subagent in a separate goroutine and returns a message indicating its creation.
// The subagent will run for a limited number of iterations and report its final answer via the message bus.
func (sm *SubagentManager) Spawn(ctx context.Context, task, label string, tc ToolContext) (string, error) {
	// Resolve per-agent provider, model, and message builder for this label.
	agentProvider, agentModel, agentMsgBuilder, agentSysPrompt := sm.ResolveAgent(label)

	sm.mu.RLock()
	subagentTools := sm.tools
	sm.mu.RUnlock()

	if subagentTools == nil {
		return "", fmt.Errorf("subagent tools not set")
	}

	logSource := fmt.Sprintf("subagent:%s", label)

	// Build initial messages using the per-agent builder, falling back to default.
	var subagentMessages []providers.Message
	if agentMsgBuilder != nil {
		subagentMessages = agentMsgBuilder(nil, "", task, nil, tc.Channel, tc.ChatID)
	} else {
		stub := &ContextBuilder{workspace: sm.workspace}
		subagentMessages = stub.BuildMessages(nil, "", task, nil, tc.Channel, tc.ChatID)
	}
	// Inject role-specific system prompt when the agent entry carries one.
	if agentSysPrompt != "" && len(subagentMessages) > 0 && subagentMessages[0].Role == "system" {
		subagentMessages[0].Content += "\n\n## Role-Specific Instructions\n\n" + agentSysPrompt
	}

	go func() {
		// Resolve the async callback context — prefer the agent's root context so
		// the goroutine is cancelled cleanly on Stop().  Fall back to
		// context.Background() for call sites that have not wired AsyncCtx.
		asyncCtx := tc.AsyncCtx
		if asyncCtx == nil {
			asyncCtx = context.Background()
		}

		// Recover from panics in subagent goroutine.
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorCF(logSource, "Subagent panicked", map[string]interface{}{"error": fmt.Sprintf("%v", r)})
				if tc.Async != nil { // Use tc.Async
					tc.Async(asyncCtx, ErrorResult(fmt.Sprintf("Subagent panicked: %v", r)))
				}
			}
		}()

		logger.InfoCF(logSource, "Subagent spawned", map[string]interface{}{"task": task, "label": label})

		// Create a new ToolLoopConfig for the subagent using the per-agent provider/model.
		toolLoopConfig := ToolLoopConfig{
			Provider:      agentProvider,
			Model:         agentModel,
			Tools:         subagentTools,
			MaxIterations: 10,
			LLMOptions:    nil,
		}

		// Pass the ToolContext directly to RunToolLoop
		loopResult, err := RunToolLoop(ctx, toolLoopConfig, subagentMessages, tc)
		if err != nil {
			logger.ErrorCF(logSource, "Subagent failed", map[string]interface{}{"error": err.Error()})
			if tc.Async != nil {
				tc.Async(asyncCtx, ErrorResult(fmt.Sprintf("Subagent failed: %v", err)))
			}
			return
		}

		logger.InfoCF(logSource, "Subagent completed", map[string]interface{}{"iterations": loopResult.Iterations})

		if tc.Async != nil {
			tc.Async(asyncCtx, NewToolResult(loopResult.Content))
		} else {
			logger.InfoCF(logSource, "Subagent completed without explicit callback", map[string]interface{}{"result": loopResult.Content})
		}
	}()

	return fmt.Sprintf("Subagent '%s' spawned for task: %s", label, task), nil
}

// RunToolLoop executes the LLM + tool call iteration loop for synchronous subagents.
func (sm *SubagentManager) RunToolLoop(ctx context.Context, task, label, channel, chatID, sessionKey string) (*ToolLoopResult, error) {
	// Resolve per-agent provider, model, and message builder for this label.
	agentProvider, agentModel, agentMsgBuilder, agentSysPrompt := sm.ResolveAgent(label)

	sm.mu.RLock()
	subagentTools := sm.tools
	sm.mu.RUnlock()

	if subagentTools == nil {
		return nil, fmt.Errorf("subagent tools not set")
	}

	// Build initial messages using the per-agent builder, falling back to default.
	var subagentMessages []providers.Message
	if agentMsgBuilder != nil {
		subagentMessages = agentMsgBuilder(nil, "", task, nil, channel, chatID)
	} else {
		stub := &ContextBuilder{workspace: sm.workspace}
		subagentMessages = stub.BuildMessages(nil, "", task, nil, channel, chatID)
	}
	// Inject role-specific system prompt when the agent entry carries one.
	if agentSysPrompt != "" && len(subagentMessages) > 0 && subagentMessages[0].Role == "system" {
		subagentMessages[0].Content += "\n\n## Role-Specific Instructions\n\n" + agentSysPrompt
	}

	toolLoopConfig := ToolLoopConfig{
		Provider:      agentProvider,
		Model:         agentModel,
		Tools:         subagentTools,
		MaxIterations: 10,
		LLMOptions:    nil,
	}

	// Create a ToolContext for the synchronous subagent loop
	tc := ToolContext{
		Channel:    channel,
		ChatID:     chatID,
		SessionKey: sessionKey,
		// No Async callback needed as this is synchronous
		Bus: sm.bus,
	}

	loopResult, err := RunToolLoop(ctx, toolLoopConfig, subagentMessages, tc) // Pass ToolContext
	if err != nil {
		return nil, fmt.Errorf("subagent failed: %w", err)
	}
	return loopResult, nil
}

// RegisteredRoles returns the roles of all registered team agents for delegate_task discovery.
func (sm *SubagentManager) RegisteredRoles() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	roles := make([]string, 0, len(sm.roleToID))
	for role := range sm.roleToID {
		roles = append(roles, role)
	}
	return roles
}

func (sm *SubagentManager) CLIProviders() map[string]providers.LLMProvider {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	// Return a shallow copy so callers cannot inadvertently mutate the internal map.
	copy := make(map[string]providers.LLMProvider, len(sm.cliProviders))
	for k, v := range sm.cliProviders {
		copy[k] = v
	}
	return copy
}

// ContextBuilder (placeholder for the actual implementation in agent/context.go)
// It's defined here because SubagentManager (in tools) needs it.
type ContextBuilder struct {
	workspace string
	// Removed logger *logger.Logger to avoid undefined error with placeholder
	// Add other necessary fields as per agent/context.go
}

// BuildMessages (placeholder — used as fallback when no injected builder is available)
func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary, userMessage string, media []string, channel, chatID string) []providers.Message {
	// Minimal stub: produces a bare prompt with no agent identity or memory.
	messages := []providers.Message{}
	if summary != "" {
		messages = append(messages, providers.Message{Role: "system", Content: "Summary: " + summary})
	}
	if userMessage != "" {
		messages = append(messages, providers.Message{Role: "user", Content: userMessage})
	}
	return messages
}

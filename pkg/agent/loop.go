// License: MIT

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/channels"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/constants"
	"github.com/Vatthu/vikram/pkg/epistemology"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/proactive"
	"github.com/Vatthu/vikram/pkg/proactive/sop"
	"github.com/Vatthu/vikram/pkg/providers"
	"github.com/Vatthu/vikram/pkg/session"
	"github.com/Vatthu/vikram/pkg/state"
	"github.com/Vatthu/vikram/pkg/tools"
	"github.com/Vatthu/vikram/pkg/utils"
)

type AgentLoop struct {
	bus            *bus.MessageBus
	provider       providers.LLMProvider
	workspace      string
	model          string
	modelMu        sync.RWMutex // Protects model field for concurrent goroutine access
	contextWindow  int          // Maximum context window size in tokens (max_tokens)
	temperature    float64      // Sampling temperature read from config
	maxIterations  int
	sessions       *session.SessionManager
	state          *state.Manager
	contextBuilder *ContextBuilder
	tools          *tools.ToolRegistry
	running        atomic.Bool
	summarizing    sync.Map       // Tracks which sessions are currently being summarized
	summarizeWg    sync.WaitGroup // Waits for in-flight summarizations during Stop
	channelManager *channels.Manager
	cooldowns      sync.Map // Tracks provider/model fatigue and cooldown expiration

	// rootCtx / rootCancel — cancelled when Stop() is called.
	// All goroutines spawned by the agent (summarization, async tools) should
	// derive their context from rootCtx so they exit cleanly on shutdown.
	rootCtx    context.Context
	rootCancel context.CancelFunc

	// Cached council configuration — avoids a config.json disk read on every LLM call.
	councilEnabled       bool
	councilFallbackModel string
	councilMu            sync.RWMutex
	// Pre-built provider for council cross-provider fallback.
	// Nil when council is disabled or no fallback provider is configured.
	fallbackProvider providers.LLMProvider

	// toolTimeout caps per-tool execution to prevent a single hung tool from
	// blocking the agent indefinitely. Defaults to 5 minutes.
	toolTimeout time.Duration

	// proactiveEngine learns user behavior patterns and suggests routines.
	// May be nil if initialization fails (non-critical).
	proactiveEngine *proactive.Engine

	// SubagentManager routes tasks to registered team agents with per-role models.
	SubagentManager *tools.SubagentManager
}

// llmOptions returns the LLM call options derived from config.
// contextWindow falls back to a safe 8192 if the config value is zero.
func (al *AgentLoop) llmOptions() map[string]interface{} {
	maxTok := al.contextWindow
	if maxTok <= 0 {
		maxTok = 8192
	}
	// Trust the configured temperature value unchanged.
	// DefaultConfig() sets 0.7 so zero here means the user explicitly wants 0
	// (greedy/deterministic decoding) — we must not silently override it.
	return map[string]interface{}{
		"max_tokens":  maxTok,
		"temperature": al.temperature,
	}
}

// getModel returns the current primary model name, safe for concurrent reads.
func (al *AgentLoop) getModel() string {
	al.modelMu.RLock()
	defer al.modelMu.RUnlock()
	return al.model
}

// setModel updates the primary model name, safe for concurrent writes.
func (al *AgentLoop) setModel(m string) {
	al.modelMu.Lock()
	defer al.modelMu.Unlock()
	al.model = m
}

// UpdateCouncilConfig refreshes the cached council config at runtime.
// Safe to call concurrently from any goroutine.
func (al *AgentLoop) UpdateCouncilConfig(enabled bool, fallbackModel string) {
	al.councilMu.Lock()
	defer al.councilMu.Unlock()
	al.councilEnabled = enabled
	al.councilFallbackModel = fallbackModel
}

// getCouncilConfig reads the council config under its dedicated read lock.
func (al *AgentLoop) getCouncilConfig() (enabled bool, fallbackModel string) {
	al.councilMu.RLock()
	defer al.councilMu.RUnlock()
	return al.councilEnabled, al.councilFallbackModel
}

// getFallbackProvider returns the pre-built council fallback provider (may be nil).
func (al *AgentLoop) getFallbackProvider() providers.LLMProvider {
	al.councilMu.RLock()
	defer al.councilMu.RUnlock()
	return al.fallbackProvider
}

// processOptions configures how a message is processed
type processOptions struct {
	SessionKey      string // Session identifier for history/context
	Channel         string // Target channel for tool execution
	ChatID          string // Target chat ID for tool execution
	UserMessage     string // User message content (may include prefix)
	DefaultResponse string // Response when LLM returns empty
	EnableSummary   bool   // Whether to trigger summarization
	SendResponse    bool   // Whether to send response via bus
	NoHistory       bool   // If true, don't load session history (for heartbeat)
}

// createToolRegistry creates a tool registry with common tools.
// This is shared between main agent and subagents.
func createToolRegistry(workspace string, restrict bool, cfg *config.Config, msgBus *bus.MessageBus, graphStore epistemology.GraphStore) *tools.ToolRegistry {
	registry := tools.NewToolRegistry()

	// File system tools
	registry.Register(tools.NewReadFileTool(workspace, restrict, msgBus))
	registry.Register(tools.NewWriteFileTool(workspace, restrict, msgBus))
	registry.Register(tools.NewListDirTool(workspace, restrict, msgBus))
	registry.Register(tools.NewEditFileTool(workspace, restrict))
	registry.Register(tools.NewAppendFileTool(workspace, restrict))
	registry.Register(tools.NewBootstrapTool(workspace))

	// Epistemology memory tools
	if graphStore != nil {
		registry.Register(tools.NewAssertFactTool(graphStore))
		registry.Register(tools.NewQueryGraphTool(graphStore))
	}

	// Shell execution
	execTool := tools.NewExecToolForWorkspace(workspace, restrict, cfg.Workspace.Sandboxed, msgBus)
	registry.Register(execTool)

	if searchTool := tools.NewWebSearchTool(tools.WebSearchToolOptions{
		BraveAPIKey:          cfg.Tools.Web.Brave.APIKey,
		BraveMaxResults:      cfg.Tools.Web.Brave.MaxResults,
		BraveEnabled:         cfg.Tools.Web.Brave.Enabled,
		DuckDuckGoMaxResults: cfg.Tools.Web.DuckDuckGo.MaxResults,
		DuckDuckGoEnabled:    cfg.Tools.Web.DuckDuckGo.Enabled,
		PerplexityAPIKey:     cfg.Tools.Web.Perplexity.APIKey,
		PerplexityMaxResults: cfg.Tools.Web.Perplexity.MaxResults,
		PerplexityEnabled:    cfg.Tools.Web.Perplexity.Enabled,
	}); searchTool != nil {
		registry.Register(searchTool)
	}
	registry.Register(tools.NewWebFetchTool(50000))

	// Message tool - available to both agent and subagent
	// Subagent uses it to communicate directly with user
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, content string) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: content,
		})
		return nil
	})
	registry.Register(messageTool)

	return registry
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	restrict := cfg.Agents.Defaults.RestrictToWorkspace

	// Initialize vectorless epistemology memory
	var graphStore epistemology.GraphStore
	var err error
	graphStore, err = epistemology.NewSQLiteGraphStore(workspace)
	if err != nil {
		logger.ErrorCF("agent", "Failed to initialize epistemology store (sqlite)", map[string]interface{}{"error": err.Error()})
		// Fallback to JSON if SQLite fails (graceful degradation)
		if gJSON, errJSON := epistemology.NewJSONGraphStore(workspace); errJSON == nil {
			graphStore = gJSON
		}
	}

	// Create tool registry for main agent
	toolsRegistry := createToolRegistry(workspace, restrict, cfg, msgBus, graphStore)

	// Create subagent manager with its own tool registry.
	cliProviders := make(map[string]providers.LLMProvider)
	subagentManager := tools.NewSubagentManager(provider, cfg.Agents.Defaults.Model, workspace, msgBus, cliProviders)
	subagentTools := createToolRegistry(workspace, restrict, cfg, msgBus, graphStore)
	subagentManager.SetTools(subagentTools)

	// Register each configured agent as a team member with its own provider,
	// model, workspace, and system prompt so Spawn/RunToolLoop can route to
	// the right model for each role.
	var teamRoleDescs []string
	for _, agentCfg := range cfg.Agents.List {
		agentProvider := provider
		agentModel := cfg.Agents.Defaults.Model
		var agentMsgBuilder tools.MessageBuilderFn

		if agentCfg.Provider != "" && agentCfg.Model != "" {
			if p, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model); err == nil {
				agentProvider = p
				agentModel = agentCfg.Model
				logger.InfoCF("agent", "Registered agent provider", map[string]interface{}{
					"agent_id": agentCfg.ID,
					"role":     agentCfg.Role,
					"provider": agentCfg.Provider,
					"model":    agentCfg.Model,
				})
			} else {
				logger.WarnCF("agent", "Failed to create per-agent provider, using default", map[string]interface{}{
					"agent_id": agentCfg.ID,
					"error":    err.Error(),
				})
			}
		}

		// When the agent has its own workspace, create a per-workspace ContextBuilder
		// so the agent gets role-specific AGENT.md, SOUL.md, memory etc.
		agentWs := agentCfg.Workspace
		if agentWs == "" {
			agentWs = workspace
		}
		if agentWs != workspace {
			wsCb := NewContextBuilder(agentWs)
			wsCb.SetToolsRegistry(subagentTools)
			wsCb.SetGraphStore(graphStore)
			agentMsgBuilder = wsCb.BuildMessages
		}

		subagentManager.RegisterAgent(agentCfg.ID, agentCfg.Role, agentProvider, agentModel, agentMsgBuilder, agentCfg.SystemPrompt)
		if agentCfg.Role != "" {
			teamRoleDescs = append(teamRoleDescs, fmt.Sprintf("- **%s** (`%s`): %s (model: %s)",
				agentCfg.Role, agentCfg.ID, agentCfg.Name, agentCfg.Model))
		}
	}

	// Register spawn tool (for main agent)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)

	// Register subagent tool (synchronous execution)
	subagentTool := tools.NewSubagentTool(subagentManager)
	toolsRegistry.Register(subagentTool)

	// Register SOP tool
	sopOrchestrator := sop.NewOrchestrator(subagentManager, msgBus, cfg.Agents.List)
	sopTool := NewSOPTool(sopOrchestrator)
	toolsRegistry.Register(sopTool)

	// Register DelegateTaskTool for advanced orchestration
	delegateTool := tools.NewDelegateTaskTool(subagentManager)
	toolsRegistry.Register(delegateTool)

	// Register notify_user tool (main agent only — subagents must not send completion notifications)
	notifyTool := tools.NewNotifyUserTool()
	toolsRegistry.Register(notifyTool)

	sessionsManager := session.NewSessionManager(filepath.Join(workspace, "sessions"))

	// Create state manager for atomic state persistence
	stateManager := state.NewManager(workspace)

	// Create context builder and set tools registry
	contextBuilder := NewContextBuilder(workspace)
	contextBuilder.SetToolsRegistry(toolsRegistry)
	contextBuilder.SetGraphStore(graphStore)

	// Inject the real BuildMessages function so subagents receive a fully-hydrated
	// system prompt (identity, memory, skills) instead of the bare stub fallback.
	subagentManager.SetMessageBuilder(contextBuilder.BuildMessages)

	// Tell the main agent which team roles are available for delegation.
	if len(teamRoleDescs) > 0 {
		contextBuilder.SetTeamRoles(strings.Join(teamRoleDescs, "\n"))
	}

	rootCtx, rootCancel := context.WithCancel(context.Background())

	al := &AgentLoop{
		bus:                  msgBus,
		provider:             provider,
		workspace:            workspace,
		model:                cfg.Agents.Defaults.Model,
		contextWindow:        cfg.Agents.Defaults.MaxTokens,
		temperature:          cfg.Agents.Defaults.Temperature,
		maxIterations:        cfg.Agents.Defaults.MaxToolIterations,
		sessions:             sessionsManager,
		state:                stateManager,
		contextBuilder:       contextBuilder,
		tools:                toolsRegistry,
		SubagentManager:      subagentManager,
		summarizing:          sync.Map{},
		cooldowns:            sync.Map{},
		councilEnabled:       cfg.Council.Enabled,
		councilFallbackModel: cfg.Council.FallbackModel,
		toolTimeout:          5 * time.Minute,
		rootCtx:              rootCtx,
		rootCancel:           rootCancel,
	}

	// Initialize proactive engine for behavior-pattern learning.
	// Non-fatal: agent works without it.
	if eng, err := proactive.NewEngine(workspace); err == nil {
		al.proactiveEngine = eng
	} else {
		logger.WarnCF("agent", "Proactive engine init failed (non-fatal)", map[string]interface{}{"error": err.Error()})
	}

	// Initialize council fallback provider if configured.
	// Non-fatal: without it, council falls back to using primary provider with fallback model.
	if cfg.Council.Enabled && cfg.Council.Fallback != "" && cfg.Council.FallbackModel != "" {
		if fp, err := providers.CreateProviderForFallback(cfg, cfg.Council.Fallback, cfg.Council.FallbackModel); err == nil {
			al.fallbackProvider = fp
			logger.InfoCF("agent", "Council fallback provider initialized", map[string]interface{}{
				"fallback_provider": cfg.Council.Fallback,
				"fallback_model":    cfg.Council.FallbackModel,
			})
		} else {
			logger.WarnCF("agent", "Council fallback provider init failed (non-fatal)", map[string]interface{}{
				"provider": cfg.Council.Fallback,
				"error":    err.Error(),
			})
		}
	}

	return al
}

// ProactiveEngine returns the proactive engine instance (may be nil).
func (al *AgentLoop) ProactiveEngine() *proactive.Engine {
	return al.proactiveEngine
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running.Store(true)
	sem := make(chan struct{}, 10) // Worker pool bounds concurrent LLM calls

	for al.running.Load() {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			sem <- struct{}{} // Acquire worker
			go func(m bus.InboundMessage) {
				defer func() { <-sem }() // Release worker

				response, err := al.processMessage(ctx, m)
				if err != nil {
					response = fmt.Sprintf("Error processing message: %v", err)
				}

				if response != "" {
					al.bus.PublishOutbound(bus.OutboundMessage{
						Channel: m.Channel,
						ChatID:  m.ChatID,
						Content: response,
					})
				}
			}(msg)
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running.Store(false)
	al.rootCancel()
	al.summarizeWg.Wait() // let any in-flight summarizations finish writing
}

func (al *AgentLoop) RegisterTool(tool tools.Tool) {
	al.tools.Register(tool)
}

func (al *AgentLoop) SetChannelManager(cm *channels.Manager) {
	al.channelManager = cm

	if msgToolRaw, ok := al.tools.Get("message"); ok {
		if msgTool, ok := msgToolRaw.(*tools.MessageTool); ok {
			msgTool.SetSendCallback(func(channel, chatID, content string) error {
				if al.channelManager != nil {
					return al.channelManager.SendToChannel(context.Background(), channel, chatID, content)
				}

				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: channel,
					ChatID:  chatID,
					Content: content,
				})
				return nil
			})
		}
	}
}

// RecordLastChannel records the last active channel for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChannel(channel string) error {
	return al.state.SetLastChannel(channel)
}

// RecordLastChatID records the last active chat ID for this workspace.
// This uses the atomic state save mechanism to prevent data loss on crash.
func (al *AgentLoop) RecordLastChatID(chatID string) error {
	return al.state.SetLastChatID(chatID)
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	return al.ProcessDirectWithChannel(ctx, content, sessionKey, "cli", "direct")
}

func (al *AgentLoop) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    channel,
		SenderID:   "cron",
		ChatID:     chatID,
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

// ProcessHeartbeat processes a heartbeat request without session history.
// Each heartbeat is independent and doesn't accumulate context.
func (al *AgentLoop) ProcessHeartbeat(ctx context.Context, content, channel, chatID string) (string, error) {
	return al.runAgentLoop(ctx, processOptions{
		SessionKey:      "heartbeat",
		Channel:         channel,
		ChatID:          chatID,
		UserMessage:     content,
		DefaultResponse: "I've completed processing but have no response to give.",
		EnableSummary:   false,
		SendResponse:    false,
		NoHistory:       true, // Don't load session history for heartbeat
	})
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Add message preview to log (show full content for error messages)
	var logContent string
	if strings.Contains(msg.Content, "Error:") || strings.Contains(msg.Content, "error") {
		logContent = msg.Content // Full content for errors
	} else {
		logContent = utils.Truncate(msg.Content, 80)
	}
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, logContent),
		map[string]interface{}{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Check for commands
	if response, handled := al.handleCommand(ctx, msg); handled {
		return response, nil
	}

	// Process as user message
	return al.runAgentLoop(ctx, processOptions{
		SessionKey:      msg.SessionKey,
		Channel:         msg.Channel,
		ChatID:          msg.ChatID,
		UserMessage:     msg.Content,
		DefaultResponse: "I've completed processing but have no response to give.",
		EnableSummary:   true,
		SendResponse:    false,
	})
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]interface{}{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin channel and chat_id from chat_id field (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Extract subagent result from message content
	// Format: "Task 'label' completed.\n\nResult:\n<actual content>"
	content := msg.Content
	if idx := strings.Index(content, "Result:\n"); idx >= 0 {
		content = content[idx+8:] // Extract just the result part
	}

	// Skip internal channels — only log
	if constants.IsInternalChannel(originChannel) {
		logger.InfoCF("agent", "Subagent completed (internal channel)",
			map[string]interface{}{
				"sender_id":   msg.SenderID,
				"content_len": len(content),
				"channel":     originChannel,
			})
		return "", nil
	}

	logger.InfoCF("agent", "Subagent completed",
		map[string]interface{}{
			"sender_id":   msg.SenderID,
			"channel":     originChannel,
			"content_len": len(content),
		})

	// For CLI workers (delegate_task), forward the result directly to the user.
	// Standard subagents communicate with the user via their own message tool.
	if strings.HasPrefix(msg.SenderID, "worker:") && al.bus != nil {
		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: originChannel,
			ChatID:  originChatID,
			Content: content,
		})
	}

	return "", nil
}

// runAgentLoop is the core message processing logic.
// It handles context building, LLM calls, tool execution, and response handling.
func (al *AgentLoop) runAgentLoop(ctx context.Context, opts processOptions) (string, error) {
	// 0. Record last channel for heartbeat notifications (skip internal channels)
	if opts.Channel != "" && opts.ChatID != "" {
		// Don't record internal channels (cli, system, subagent)
		if !constants.IsInternalChannel(opts.Channel) {
			channelKey := fmt.Sprintf("%s:%s", opts.Channel, opts.ChatID)
			if err := al.RecordLastChannel(channelKey); err != nil {
				logger.WarnCF("agent", "Failed to record last channel: %v", map[string]interface{}{"error": err.Error()})
			}
			// Feed interaction into the proactive behavior-learning engine.
			if al.proactiveEngine != nil {
				al.proactiveEngine.RecordActivity(opts.UserMessage, opts.Channel)
			}
		}
	}

	// 1. Tool contexts are now passed explicitly via ToolContext
	// al.updateToolContexts(opts.Channel, opts.ChatID)

	// 2. Build messages (skip history for heartbeat)
	var history []providers.Message
	var summary string
	if !opts.NoHistory {
		history = al.sessions.GetHistory(opts.SessionKey)
		summary = al.sessions.GetSummary(opts.SessionKey)
	}
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		opts.UserMessage,
		nil,
		opts.Channel,
		opts.ChatID,
	)

	// 3. Save user message to session
	al.sessions.AddMessage(opts.SessionKey, "user", opts.UserMessage)

	// 4. Run LLM iteration loop
	finalContent, iteration, terminalToolReply, err := al.runLLMIteration(ctx, messages, opts)
	if err != nil {
		return "", err
	}

	// If last tool had ForUser content and we already sent it, we might not need to send final response
	// This is controlled by the tool's Silent flag and ForUser content

	// 5. Handle empty response
	if finalContent == "" && !terminalToolReply {
		finalContent = opts.DefaultResponse
	}

	// 6. Save final assistant message to session
	if finalContent != "" {
		al.sessions.AddMessage(opts.SessionKey, "assistant", finalContent)
	}
	al.sessions.Save(opts.SessionKey)

	// 7. Optional: summarization
	if opts.EnableSummary {
		al.maybeSummarize(opts.SessionKey, opts.Channel, opts.ChatID)
	}

	// 8. Optional: send response via bus
	if opts.SendResponse && finalContent != "" {
		al.bus.PublishOutbound(bus.OutboundMessage{
			Channel: opts.Channel,
			ChatID:  opts.ChatID,
			Content: finalContent,
		})
	}

	// 9. Log response
	responsePreview := utils.Truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response: %s", responsePreview),
		map[string]interface{}{
			"session_key":  opts.SessionKey,
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}

// runLLMIteration executes the LLM call loop with tool handling.
// Returns the final content, iteration count, and any error.
func (al *AgentLoop) runLLMIteration(ctx context.Context, messages []providers.Message, opts processOptions) (string, int, bool, error) {
	iteration := 0
	var finalContent string
	var terminalToolReply bool

	// Tool loop detector prevents the agent from spinning on the same tool call.
	loopDetector := NewToolLoopDetector()

	for iteration < al.maxIterations {
		iteration++

		logger.DebugCF("agent", "LLM iteration",
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		// Build tool definitions
		providerToolDefs := al.tools.ToProviderDefs()

		// Log LLM request details
		logger.DebugCF("agent", "LLM request",
			map[string]interface{}{
				"iteration":         iteration,
				"model":             al.getModel(),
				"messages_count":    len(messages),
				"tools_count":       len(providerToolDefs),
				"max_tokens":        al.contextWindow,
				"temperature":       al.temperature,
				"system_prompt_len": len(messages[0].Content),
			})

		// Log full messages (detailed)
		logger.DebugCF("agent", "Full LLM request",
			map[string]interface{}{
				"iteration":     iteration,
				"messages_json": formatMessagesForLog(messages),
				"tools_json":    formatToolsForLog(providerToolDefs),
			})

		var response *providers.LLMResponse
		var err error

		// Read current model once per iteration — never mutate al.model from goroutines.
		// callModel may be overridden to the fallback during council recovery.
		currentModel := al.getModel()

		// Retry loop for context/token errors
		maxRetries := 2
		for retry := 0; retry <= maxRetries; retry++ {
			// --- Council Fallback Implementation ---
			// 1. Check Cooldown Cache BEFORE executing Primary
			var skipPrimary bool
			if cdTime, exists := al.cooldowns.Load(currentModel); exists {
				expiration, ok := cdTime.(time.Time)
				if ok {
					if expiration.IsZero() {
						skipPrimary = true
						logger.WarnCF("agent", fmt.Sprintf("Council: Skipping Primary %s — infinite cooldown (auth/quota)", currentModel), nil)
					} else if time.Now().Before(expiration) {
						skipPrimary = true
						logger.WarnCF("agent", fmt.Sprintf("Council: Skipping Primary %s — cooldown expires in %v", currentModel, time.Until(expiration).Round(time.Second)), nil)
					} else {
						al.cooldowns.Delete(currentModel)
					}
				}
			}

			// callModel is the model actually sent to the provider this call.
			// It may differ from currentModel during fallback — never writes back to the struct.
			callModel := currentModel

			if !skipPrimary {
				response, err = al.provider.Chat(ctx, messages, providerToolDefs, callModel, al.llmOptions())
			} else {
				err = fmt.Errorf("council: primary %s skipped due to active cooldown cache", currentModel)
			}

			// 2. Council recovery when the primary call failed.
			if err != nil {
				councilEnabled, councilFallback := al.getCouncilConfig()
				if councilEnabled && councilFallback != "" && councilFallback != currentModel {
					errStr := strings.ToLower(err.Error())

					isAuthError := strings.Contains(errStr, "401") || strings.Contains(errStr, "403") ||
						strings.Contains(errStr, "unauthorized") || strings.Contains(errStr, "forbidden")
					isRateLimit := strings.Contains(errStr, "429") ||
						strings.Contains(errStr, "too many requests") || strings.Contains(errStr, "quota") ||
						strings.Contains(errStr, "exhausted") || strings.Contains(errStr, "rate limit")
					isOverloaded := strings.Contains(errStr, "503") || strings.Contains(errStr, "timeout") ||
						strings.Contains(errStr, "service unavailable")
					isCacheSkip := strings.Contains(errStr, "active cooldown cache")

					if isAuthError || isRateLimit || isOverloaded || isCacheSkip {
						if !isCacheSkip {
							var notifyMsg string
							switch {
							case isAuthError:
								al.cooldowns.Store(currentModel, time.Time{}) // zero value = infinite cooldown
								notifyMsg = fmt.Sprintf("⚠️ The API key for `%s` is unauthorised or expired (401/403). Shifting to fallback model (`%s`). Please check your billing.", currentModel, councilFallback)
							case isRateLimit:
								al.cooldowns.Store(currentModel, time.Now().Add(5*time.Minute))
								notifyMsg = fmt.Sprintf("⚠️ Rate limit / quota exceeded on `%s` (429). Cooling down for 5 minutes and shifting to fallback (`%s`).", currentModel, councilFallback)
							case isOverloaded:
								al.cooldowns.Store(currentModel, time.Now().Add(1*time.Minute))
								notifyMsg = fmt.Sprintf("⚠️ `%s` servers are overloaded (503). Retrying on fallback model (`%s`).", currentModel, councilFallback)
							}
							if notifyMsg != "" && !constants.IsInternalChannel(opts.Channel) {
								al.bus.PublishOutbound(bus.OutboundMessage{
									Channel: opts.Channel,
									ChatID:  opts.ChatID,
									Content: notifyMsg,
								})
							}
							logger.WarnCF("agent", "Council redirecting to fallback", map[string]interface{}{
								"primary":  currentModel,
								"fallback": councilFallback,
								"reason":   errStr,
							})
						}

						// Execute fallback using the pre-built fallback provider (correct API endpoint).
						callModel = councilFallback
						fallbackProv := al.getFallbackProvider()
						if fallbackProv == nil {
							// No dedicated fallback provider — use primary with fallback model (best effort).
							fallbackProv = al.provider
						}
						response, err = fallbackProv.Chat(ctx, messages, providerToolDefs, callModel, al.llmOptions())
					}
				}
			}
			// --- End Council Implementation ---

			if err == nil {
				break // Success
			}

			errMsg := strings.ToLower(err.Error())

			// Check if model doesn't support tool calling — retry without tools
			isToolError := strings.Contains(errMsg, "tool calling is not supported") ||
				strings.Contains(errMsg, "tools are not supported") ||
				strings.Contains(errMsg, "does not support tools") ||
				strings.Contains(errMsg, "does not support function")
			if isToolError && retry < maxRetries {
				logger.WarnCF("agent", "Model does not support tool calling, retrying without tools", map[string]interface{}{
					"error": err.Error(),
					"retry": retry,
				})
				providerToolDefs = nil
				continue
			}

			// Check for context window errors (provider specific, but usually contain "token" or "invalid")
			isContextError := strings.Contains(errMsg, "token") ||
				strings.Contains(errMsg, "context") ||
				strings.Contains(errMsg, "invalidparameter") ||
				strings.Contains(errMsg, "length")

			if isContextError && retry < maxRetries {
				logger.WarnCF("agent", "Context window error detected, attempting compression", map[string]interface{}{
					"error": err.Error(),
					"retry": retry,
				})

				// Notify user on first retry only
				if retry == 0 && !constants.IsInternalChannel(opts.Channel) {
					al.bus.PublishOutbound(bus.OutboundMessage{
						Channel: opts.Channel,
						ChatID:  opts.ChatID,
						Content: "⚠️ Context window exceeded. Compressing history and retrying...",
					})
				}

				// Force compression
				al.forceCompression(opts.SessionKey)

				// Rebuild messages with compressed history.
				// After forceCompression, session history already contains the current
				// user message (added in step 3 of runAgentLoop), so we pass an empty
				// currentMessage to BuildMessages to avoid duplicating it.
				newHistory := al.sessions.GetHistory(opts.SessionKey)
				newSummary := al.sessions.GetSummary(opts.SessionKey)

				messages = al.contextBuilder.BuildMessages(
					newHistory,
					newSummary,
					"",
					nil,
					opts.Channel,
					opts.ChatID,
				)

				continue
			}

			// Real error or success, break loop
			break
		}

		if err != nil {
			logger.ErrorCF("agent", "LLM call failed",
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", iteration, false, fmt.Errorf("LLM call failed after retries: %w", err)
		}

		// Check if no tool calls - we're done
		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
				map[string]interface{}{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		// Log tool calls
		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("agent", "LLM requested tool calls",
			map[string]interface{}{
				"tools":     toolNames,
				"count":     len(response.ToolCalls),
				"iteration": iteration,
			})

		// Build assistant message with tool calls
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

		// Save assistant message with tool calls to session
		al.sessions.AddFullMessage(opts.SessionKey, assistantMsg)

		onlyTerminalToolCalls := len(response.ToolCalls) > 0
		var terminalToolSummary string

		// Execute tool calls
		for _, tc := range response.ToolCalls {
			if !isTerminalToolCall(tc.Name) {
				onlyTerminalToolCalls = false
			}

			// Log tool call with arguments preview
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := utils.Truncate(string(argsJSON), 200)
			logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
				map[string]interface{}{
					"tool":      tc.Name,
					"iteration": iteration,
				})

			// Record the call BEFORE execution so the detector sees it even if the tool panics.
			loopArgsHash := loopDetector.Record(tc.Name, tc.Arguments)

			// Create async callback for tools that implement AsyncTool
			// NOTE: Following vikram's design, async tools do NOT send results directly to users.
			// Instead, they notify the agent via PublishInbound, and the agent decides
			// whether to forward the result to the user (in processSystemMessage).
			asyncCallback := func(callbackCtx context.Context, result *tools.ToolResult) {
				// Log the async completion but don't send directly to user
				// The agent will handle user notification via processSystemMessage
				if !result.Silent && result.ForUser != "" {
					logger.InfoCF("agent", "Async tool completed, agent will handle notification",
						map[string]interface{}{
							"tool":        tc.Name,
							"content_len": len(result.ForUser),
						})
				}
			}

			toolCtx, toolCancel := context.WithTimeout(ctx, al.toolTimeout)
			toolResult := al.tools.ExecuteWithContext(toolCtx, tc.Name, tc.Arguments, tools.ToolContext{
				Channel:    opts.Channel,
				ChatID:     opts.ChatID,
				SessionKey: opts.SessionKey,
				SenderID:   "", // SenderID is generally unused by subagents here
				Bus:        al.bus,
				Async:      asyncCallback,
				AsyncCtx:   al.rootCtx,
			})
			toolCancel()

			if tc.Name == "complete_bootstrap" {
				terminalToolSummary = "Bootstrap completed. I’ve saved the learned identity into the workspace and future sessions will wake from it."
			}

			// Send ForUser content to user immediately if not Silent
			if !toolResult.Silent && toolResult.ForUser != "" && opts.SendResponse {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: opts.Channel,
					ChatID:  opts.ChatID,
					Content: toolResult.ForUser,
				})
				logger.DebugCF("agent", "Sent tool result to user",
					map[string]interface{}{
						"tool":        tc.Name,
						"content_len": len(toolResult.ForUser),
					})
			}

			// Determine content for LLM based on tool result.
			contentForLLM := toolResult.ForLLM
			if contentForLLM == "" && toolResult.Err != nil {
				contentForLLM = toolResult.Err.Error()
			}

			// Hard-cap tool results so one large response cannot blow out context.
			const maxToolResultChars = 50_000
			const toolResultTruncSuffix = "\n\n\u26a0 [Content truncated \u2014 original exceeded size limit. Use offset/limit parameters or request specific sections for large results.]"
			if len(contentForLLM) > maxToolResultChars {
				contentForLLM = contentForLLM[:maxToolResultChars] + toolResultTruncSuffix
			}

			// Record outcome for loop detection (same args + same result = no progress).
			loopDetector.RecordOutcome(loopArgsHash, contentForLLM)

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    contentForLLM,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)

			// Save tool result message to session
			al.sessions.AddFullMessage(opts.SessionKey, toolResultMsg)
		}

		if onlyTerminalToolCalls {
			terminalToolReply = true
			if finalContent == "" && terminalToolSummary != "" {
				finalContent = terminalToolSummary
			}
			break
		}

		// ── Tool loop detection ──────────────────────────────────────────────
		// Evaluated once after ALL tool calls in this iteration are complete.
		if det := loopDetector.Check(); det.Severity > LoopNone {
			logger.WarnCF("agent", "Tool loop detected",
				map[string]interface{}{
					"kind":      det.Kind,
					"severity":  det.Severity,
					"iteration": iteration,
				})
			if det.Severity == LoopCritical {
				// Hard-stop: return agent's message to the user.
				finalContent = det.Message
				break
			}
			// Warning: inject a system note so the LLM self-corrects next turn.
			messages = append(messages, providers.Message{
				Role:    "user",
				Content: "[System note: " + det.Message + "]",
			})
		}
	}

	return finalContent, iteration, terminalToolReply, nil
}

func isTerminalToolCall(name string) bool {
	switch name {
	case "message", "complete_bootstrap":
		return true
	default:
		return false
	}
}

// maybeSummarize triggers summarization if the session history exceeds thresholds.
func (al *AgentLoop) maybeSummarize(sessionKey, channel, chatID string) {
	newHistory := al.sessions.GetHistory(sessionKey)
	tokenEstimate := al.estimateTokens(newHistory)
	threshold := al.contextWindow * 75 / 100

	if len(newHistory) > 20 || tokenEstimate > threshold {
		if _, loading := al.summarizing.LoadOrStore(sessionKey, true); !loading {
			al.summarizeWg.Add(1)
			go func() {
				defer al.summarizeWg.Done()
				defer al.summarizing.Delete(sessionKey)
				// Notify user about optimization if not an internal channel
				if !constants.IsInternalChannel(channel) {
					al.bus.PublishOutbound(bus.OutboundMessage{
						Channel: channel,
						ChatID:  chatID,
						Content: "⚠️ Memory threshold reached. Optimizing conversation history...",
					})
				}
				al.summarizeSession(sessionKey)
			}()
		}
	}
}

// forceCompression aggressively reduces context when the token limit is hit.
// Session history never contains a system message — that is rebuilt fresh on
// every call from BuildMessages.  We simply drop the oldest half of real
// conversation messages and record the gap as a synthetic user note so the
// LLM knows context was truncated.
func (al *AgentLoop) forceCompression(sessionKey string) {
	history := al.sessions.GetHistory(sessionKey)
	if len(history) <= 4 {
		return
	}

	// Drop the first 50 % of messages.  Round up so we always keep at least
	// ceil(N/2) messages, preserving the most-recent context.
	dropCount := len(history) / 2
	kept := history[dropCount:]

	// Prepend a synthetic note so the LLM is aware of the gap.
	compressionNote := providers.Message{
		Role:    "user",
		Content: fmt.Sprintf("[System note: %d older messages were dropped due to context window limits. Conversation continues from here.]", dropCount),
	}
	newHistory := make([]providers.Message, 0, 1+len(kept))
	newHistory = append(newHistory, compressionNote)
	newHistory = append(newHistory, kept...)

	al.sessions.SetHistory(sessionKey, newHistory)
	al.sessions.Save(sessionKey)

	logger.WarnCF("agent", "Forced compression executed", map[string]interface{}{
		"session_key":  sessionKey,
		"dropped_msgs": dropCount,
		"new_count":    len(newHistory),
	})
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(tools),
		"names": tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

// formatMessagesForLog formats messages for logging
func formatMessagesForLog(messages []providers.Message) string {
	if len(messages) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, msg := range messages {
		result += fmt.Sprintf("  [%d] Role: %s\n", i, msg.Role)
		if len(msg.ToolCalls) > 0 {
			result += "  ToolCalls:\n"
			for _, tc := range msg.ToolCalls {
				result += fmt.Sprintf("    - ID: %s, Type: %s, Name: %s\n", tc.ID, tc.Type, tc.Name)
				if tc.Function != nil {
					result += fmt.Sprintf("      Arguments: %s\n", utils.Truncate(tc.Function.Arguments, 200))
				}
			}
		}
		if msg.Content != "" {
			content := utils.Truncate(msg.Content, 200)
			result += fmt.Sprintf("  Content: %s\n", content)
		}
		if msg.ToolCallID != "" {
			result += fmt.Sprintf("  ToolCallID: %s\n", msg.ToolCallID)
		}
		result += "\n"
	}
	result += "]"
	return result
}

// formatToolsForLog formats tool definitions for logging
func formatToolsForLog(tools []providers.ToolDefinition) string {
	if len(tools) == 0 {
		return "[]"
	}

	var result string
	result += "[\n"
	for i, tool := range tools {
		result += fmt.Sprintf("  [%d] Type: %s, Name: %s\n", i, tool.Type, tool.Function.Name)
		result += fmt.Sprintf("      Description: %s\n", tool.Function.Description)
		if len(tool.Function.Parameters) > 0 {
			result += fmt.Sprintf("      Parameters: %s\n", utils.Truncate(fmt.Sprintf("%v", tool.Function.Parameters), 200))
		}
	}
	result += "]"
	return result
}

// summarizeSession summarizes the conversation history for a session.
func (al *AgentLoop) summarizeSession(sessionKey string) {
	ctx, cancel := context.WithTimeout(al.rootCtx, 120*time.Second)
	defer cancel()

	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)

	// Keep last 4 messages for continuity
	if len(history) <= 4 {
		return
	}

	toSummarize := history[:len(history)-4]

	// Oversized Message Guard
	// Skip messages larger than 50% of context window to prevent summarizer overflow
	maxMessageTokens := al.contextWindow / 2
	validMessages := make([]providers.Message, 0)
	omitted := false

	for _, m := range toSummarize {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		// Estimate tokens for this message
		msgTokens := len(m.Content) / 2 // Use safer estimate here too (2.5 -> 2 for integer division safety)
		if msgTokens > maxMessageTokens {
			omitted = true
			continue
		}
		validMessages = append(validMessages, m)
	}

	if len(validMessages) == 0 {
		return
	}

	// Multi-Part Summarization
	// Split into two parts if history is significant
	var finalSummary string
	if len(validMessages) > 10 {
		mid := len(validMessages) / 2
		part1 := validMessages[:mid]
		part2 := validMessages[mid:]

		s1, _ := al.summarizeBatch(ctx, part1, "")
		s2, _ := al.summarizeBatch(ctx, part2, "")

		// Merge them
		mergePrompt := fmt.Sprintf("Merge these two conversation summaries into one cohesive summary:\n\n1: %s\n\n2: %s", s1, s2)
		resp, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: mergePrompt}}, nil, al.getModel(), map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		if err == nil {
			finalSummary = resp.Content
		} else {
			finalSummary = s1 + " " + s2
		}
	} else {
		finalSummary, _ = al.summarizeBatch(ctx, validMessages, summary)
	}

	if omitted && finalSummary != "" {
		finalSummary += "\n[Note: Some oversized messages were omitted from this summary for efficiency.]"
	}

	if finalSummary != "" {
		// Atomically update summary and truncate history in one operation to
		// prevent a race where a new message arrives between the two calls.
		al.sessions.SummarizeAndTruncate(sessionKey, finalSummary, 4)
		al.sessions.Save(sessionKey)
	}
}

// summarizeBatch summarizes a batch of messages.
func (al *AgentLoop) summarizeBatch(ctx context.Context, batch []providers.Message, existingSummary string) (string, error) {
	prompt := "Provide a concise summary of this conversation segment, preserving core context and key points.\n"
	if existingSummary != "" {
		prompt += "Existing context: " + existingSummary + "\n"
	}
	prompt += "\nCONVERSATION:\n"
	for _, m := range batch {
		prompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	response, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: prompt}}, nil, al.getModel(), map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

// estimateTokens estimates the number of tokens in a message list.
// Uses a safe heuristic of 2.5 characters per token to account for CJK and other
// overheads better than the previous 3 chars/token.
func (al *AgentLoop) estimateTokens(messages []providers.Message) int {
	totalChars := 0
	for _, m := range messages {
		totalChars += utf8.RuneCountInString(m.Content)
		// Count ToolCall argument bytes — large tool results can dominate token usage.
		for _, tc := range m.ToolCalls {
			if tc.Function != nil {
				totalChars += utf8.RuneCountInString(tc.Function.Arguments)
			}
		}
	}
	// 2.5 chars per token = totalChars * 2 / 5
	return totalChars * 2 / 5
}

func (al *AgentLoop) handleCommand(ctx context.Context, msg bus.InboundMessage) (string, bool) {
	content := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(content, "/") {
		return "", false
	}

	parts := strings.Fields(content)
	if len(parts) == 0 {
		return "", false
	}

	cmd := parts[0]
	args := parts[1:]

	switch cmd {
	case "/show":
		if len(args) < 1 {
			return "Usage: /show [model|channel]", true
		}
		switch args[0] {
		case "model":
			return fmt.Sprintf("Current model: %s", al.getModel()), true
		case "channel":
			return fmt.Sprintf("Current channel: %s", msg.Channel), true
		default:
			return fmt.Sprintf("Unknown show target: %s", args[0]), true
		}

	case "/list":
		if len(args) < 1 {
			return "Usage: /list [models|channels]", true
		}
		switch args[0] {
		case "models":
			// TODO: Fetch available models dynamically if possible
			return "Available models depend on your configured provider (e.g. gemini-3.1-pro-preview, claude-opus-4-6, gpt-5, llama-3.3-70b). Edit model in config.json or run: vikram configure", true
		case "channels":
			if al.channelManager == nil {
				return "Channel manager not initialized", true
			}
			channels := al.channelManager.GetEnabledChannels()
			if len(channels) == 0 {
				return "No channels enabled", true
			}
			return fmt.Sprintf("Enabled channels: %s", strings.Join(channels, ", ")), true
		default:
			return fmt.Sprintf("Unknown list target: %s", args[0]), true
		}

	case "/switch":
		if len(args) < 3 || args[1] != "to" {
			return "Usage: /switch [model|channel] to <name>", true
		}
		target := args[0]
		value := args[2]

		switch target {
		case "model":
			// Validate model name — reject URLs and suspicious patterns
			if strings.Contains(value, "://") || strings.Contains(value, "/") || strings.Contains(value, "\\") {
				return "Invalid model name — URLs and paths are not allowed", true
			}
			oldModel := al.getModel()
			al.setModel(value)
			return fmt.Sprintf("Switched model from %s to %s", oldModel, value), true
		case "channel":
			// This changes the 'default' channel for some operations, or effectively redirects output?
			// For now, let's just validate if the channel exists
			if al.channelManager == nil {
				return "Channel manager not initialized", true
			}
			if _, exists := al.channelManager.GetChannel(value); !exists && value != "cli" {
				return fmt.Sprintf("Channel '%s' not found or not enabled", value), true
			}

			// If message came from CLI, maybe we want to redirect CLI output to this channel?
			// That would require state persistence about "redirected channel"
			// For now, just acknowledged.
			return fmt.Sprintf("Switched target channel to %s (Note: this currently only validates existence)", value), true
		default:
			return fmt.Sprintf("Unknown switch target: %s", target), true
		}
	}

	return "", false
}

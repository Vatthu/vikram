package main

import (
	"context"
	"fmt"
	"net"

	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/Vatthu/vikram/pkg/agent"
	"github.com/Vatthu/vikram/pkg/api"
	"github.com/Vatthu/vikram/pkg/bus"
	"github.com/Vatthu/vikram/pkg/channels"
	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/console"
	"github.com/Vatthu/vikram/pkg/cua"
	"github.com/Vatthu/vikram/pkg/cron"
	"github.com/Vatthu/vikram/pkg/dashboard"

	"github.com/Vatthu/vikram/pkg/events"
	"github.com/Vatthu/vikram/pkg/health"
	"github.com/Vatthu/vikram/pkg/heartbeat"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/mcp"
	"github.com/Vatthu/vikram/pkg/orchestrator"
	"github.com/Vatthu/vikram/pkg/orchestratorhost"
	"github.com/Vatthu/vikram/pkg/permissions"
	"github.com/Vatthu/vikram/pkg/proactive"
	"github.com/Vatthu/vikram/pkg/providers"
	"github.com/Vatthu/vikram/pkg/queue"
	"github.com/Vatthu/vikram/pkg/state"
	"github.com/Vatthu/vikram/pkg/tools"
	devsync "github.com/Vatthu/vikram/pkg/sync"
)

func setupCronTool(ctx context.Context, agentLoop *agent.AgentLoop, msgBus *bus.MessageBus, workspace string, restrict bool, sandboxed bool, execTimeout time.Duration) *cron.CronService {
	cronStorePath := filepath.Join(workspace, "cron", "jobs.json")

	// Create cron service
	cronService := cron.NewCronService(cronStorePath, nil)

	// Create and register CronTool
	cronTool := tools.NewCronTool(cronService, agentLoop, msgBus, workspace, restrict, sandboxed, execTimeout)
	agentLoop.RegisterTool(cronTool)

	// Set the onJob handler — use the gateway lifetime context so cron jobs
	// are cancelled when the process shuts down.
	cronService.SetOnJob(func(job *cron.CronJob) (string, error) {
		result := cronTool.ExecuteJob(ctx, job)
		return result, nil
	})

	return cronService
}

func gatewayCmd() {

	// Check for --debug flag
	args := os.Args[2:]
	for _, arg := range args {
		if arg == "--debug" || arg == "-d" {
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
			break
		}
	}

	// Root context for the entire gateway lifetime.  Cancelled on Ctrl+C / SIGTERM
	// so every downstream goroutine (cron, heartbeat, agent, channels …) stops cleanly.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := loadConfig()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	// Fail fast on incomplete provider setup using the same provider-aware rules as `doctor`.
	if err := gatewayProviderConfigError(cfg); err != nil {
		fmt.Printf("\n=======================================================\n")
		fmt.Printf("❌ FATAL ERROR: Provider configuration is incomplete ❌\n")
		fmt.Printf("%s\n", err)
		fmt.Printf("=======================================================\n\n")
		os.Exit(1)
	}

	if err := validateGatewaySecurity(cfg); err != nil {
		fmt.Printf("Security configuration error: %v\n", err)
		os.Exit(1)
	}

	// Load hardware permissions from config into global registry.
	perms := permissions.Global()
	if err := perms.SetAll(map[permissions.Feature]bool{
		permissions.Camera:        cfg.Permissions.Camera,
		permissions.Microphone:    cfg.Permissions.Microphone,
		permissions.SMS:           cfg.Permissions.SMS,
		permissions.PhoneCalls:    cfg.Permissions.PhoneCalls,
		permissions.Location:      cfg.Permissions.Location,
		permissions.Clipboard:     cfg.Permissions.Clipboard,
		permissions.Sensors:       cfg.Permissions.Sensors,
		permissions.ShellHardware: cfg.Permissions.ShellHardware,
		permissions.Notifications: cfg.Permissions.Notifications,
		permissions.Screen:        cfg.Permissions.Screen,
		permissions.ComputerUse:   cfg.Permissions.ComputerUse,
	}); err != nil {
		fmt.Printf("Error setting permissions: %v\n", err)
		os.Exit(1)
	}
	perms.Freeze()
	enabledPerms := perms.EnabledFeatures()
	if len(enabledPerms) > 0 {
		names := make([]string, len(enabledPerms))
		for i, f := range enabledPerms {
			names[i] = string(f)
		}
		fmt.Printf("🔓 Permissions enabled: %s\n", strings.Join(names, ", "))
	} else {
		fmt.Println("🔒 All hardware permissions blocked (default-deny)")
	}

	provider, err := providers.CreateProvider(cfg)
	if err != nil {
		fmt.Printf("Error creating provider: %v\n", err)
		os.Exit(1)
	}

	msgBus := bus.NewMessageBus()
	agentLoop := agent.NewAgentLoop(cfg, msgBus, provider)

	// Print agent startup info
	fmt.Println("\n📦 Agent Status:")
	startupInfo := agentLoop.GetStartupInfo()
	toolsInfo := startupInfo["tools"].(map[string]interface{})
	skillsInfo := startupInfo["skills"].(map[string]interface{})
	fmt.Printf("  • Tools: %d loaded\n", toolsInfo["count"])
	fmt.Printf("  • Skills: %d/%d available\n",
		skillsInfo["available"],
		skillsInfo["total"])

	// Log to file as well
	logger.InfoCF("agent", "Agent initialized",
		map[string]interface{}{
			"tools_count":      toolsInfo["count"],
			"skills_total":     skillsInfo["total"],
			"skills_available": skillsInfo["available"],
		})
	// Load MCP tools from external servers into the agent's tool registry.
	if cfg.MCP.Enabled {
		for _, srv := range cfg.MCP.Servers {
			mcpClient, err := mcp.NewClient(ctx, mcp.ClientConfig{
				Command: srv.Command,
				Args:    srv.Args,
				Timeout: time.Duration(srv.Timeout) * time.Second,
			})
			if err != nil {
				fmt.Printf("⚠ MCP %s: %v\n", srv.Name, err)
				continue
			}
			allowed := make(map[string]bool)
			for _, t := range srv.Allowed {
				allowed[t] = true
			}
			maxOut := srv.MaxOutput
			if maxOut == 0 {
				maxOut = 50000
			}
			count := 0
			for _, td := range mcpClient.Tools() {
				if len(allowed) > 0 && !allowed[td.Name] {
					continue
				}
				agentLoop.RegisterTool(mcp.NewAdapter(mcpClient, td, srv.Prefix, maxOut))
				count++
			}
			fmt.Printf("✓ MCP %s: %d tools\n", srv.Name, count)
		}
	}

	// CUA Driver — native macOS computer-use (background GUI automation).
	if cfg.CUA.Enabled {
		cuaBridge, err := cua.NewBridge(ctx, cfg.CUA)
		if err != nil {
			fmt.Printf("⚠ CUA Driver: %v\n", err)
		} else {
			for _, t := range cuaBridge.Tools() {
				agentLoop.RegisterTool(t)
			}
			defer cuaBridge.Close()
			fmt.Printf("✓ CUA Driver: %d computer-use tools loaded\n", len(cuaBridge.Tools()))
		}
	}

	// Setup cron tool and service
	execTimeout := time.Duration(cfg.Tools.Cron.ExecTimeoutMinutes) * time.Minute
	cronService := setupCronTool(ctx, agentLoop, msgBus, cfg.WorkspacePath(), cfg.Agents.Defaults.RestrictToWorkspace, cfg.Workspace.Sandboxed, execTimeout)

	heartbeatService := heartbeat.NewHeartbeatService(
		cfg.WorkspacePath(),
		cfg.Heartbeat.Interval,
		cfg.Heartbeat.Enabled,
	)
	heartbeatService.SetBus(msgBus)
	heartbeatService.SetProactiveEngine(agentLoop.ProactiveEngine())

	// Wire proactive suggestion delivery to the last active user channel.
	if eng := agentLoop.ProactiveEngine(); eng != nil {
		eng.SetHandler(func(ctx context.Context, suggestion proactive.Suggestion) {
			lastChannel := state.NewManager(cfg.WorkspacePath()).GetLastChannel()
			if lastChannel == "" {
				return
			}
			parts := strings.SplitN(lastChannel, ":", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return
			}
			// Skip internal channels
			if parts[0] == "cli" || parts[0] == "system" || parts[0] == "subagent" {
				return
			}
			msgBus.PublishOutbound(bus.OutboundMessage{
				Channel: parts[0],
				ChatID:  parts[1],
				Content: fmt.Sprintf("💡 %s", suggestion.Message),
			})
		})
	}

	// Daily team summary — sends a status report via the same channel the user
	// last used. Runs every 6 hours or on the heartbeat interval, whichever is longer.
	go func() {
		summaryInterval := time.Duration(cfg.Heartbeat.Interval) * time.Minute
		if summaryInterval < 6*time.Hour {
			summaryInterval = 6 * time.Hour
		}
		ticker := time.NewTicker(summaryInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sendTeamSummary(cfg, msgBus)
			case <-ctx.Done():
				return
			}
		}
	}()

	heartbeatService.SetHandler(func(prompt, channel, chatID string) *tools.ToolResult {
		// Use cli:direct as fallback if no valid channel
		if channel == "" || chatID == "" {
			channel, chatID = "cli", "direct"
		}
		// Use ProcessHeartbeat - no session history, each heartbeat is independent.
		// ctx is the gateway lifetime context — cancelled on shutdown.
		response, err := agentLoop.ProcessHeartbeat(ctx, prompt, channel, chatID)
		if err != nil {
			return tools.ErrorResult(fmt.Sprintf("Heartbeat error: %v", err))
		}
		if response == "HEARTBEAT_OK" {
			return tools.SilentResult("Heartbeat OK")
		}
		// For heartbeat, always return silent - the subagent result will be
		// sent to user via processSystemMessage when the async task completes
		return tools.SilentResult(response)
	})

	channelManager, err := channels.NewManager(cfg, msgBus)
	if err != nil {
		fmt.Printf("Error creating channel manager: %v\n", err)
		os.Exit(1)
	}

	// Inject channel manager into agent loop for command handling
	agentLoop.SetChannelManager(channelManager)

	enabledChannels := channelManager.GetEnabledChannels()
	if len(enabledChannels) > 0 {
		fmt.Printf("✓ Channels enabled: %s\n", enabledChannels)
	} else {
		fmt.Println("⚠ Warning: No channels enabled")
	}

	hostSocket := orchestratorHostSocketPath()
	budget := newAgentBudget(cfg)
	budget.setNotifier(func(role string, used, limit int) {
		stateManager := state.NewManager(cfg.WorkspacePath())
		lastChannel := stateManager.GetLastChannel()
		if lastChannel == "" {
			return
		}
		parts := strings.SplitN(lastChannel, ":", 2)
		if len(parts) != 2 {
			return
		}
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: parts[0],
			ChatID:  parts[1],
			Content: fmt.Sprintf("⚠️ Budget alert: %s has used %d/%d tokens today", role, used, limit),
		})
	})

	// Wire the reviewer agent so /v1/review/change uses a different model
	// than the implementer -- the core of independent review.
	var reviewFunc orchestratorhost.ReviewFunc
	for _, agentCfg := range cfg.Agents.List {
		if agentCfg.Role == "reviewer" && agentCfg.Provider != "" && agentCfg.Model != "" {
			reviewProvider, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
			if err == nil {
				reviewModel := agentCfg.Model
				reviewFunc = func(ctx context.Context, req orchestrator.ChangeReviewRequest) (orchestrator.ChangeReviewResponse, error) {
					if err := budget.check("reviewer"); err != nil {
						return orchestrator.ChangeReviewResponse{TaskID: req.TaskID, Verdict: orchestrator.ReviewVerdictApprove, Summary: fmt.Sprintf("Budget exceeded: %v", err)}, nil
					}
					resp, err := callReviewer(ctx, reviewProvider, reviewModel, req)
					if err == nil && resp.Verdict != "" {
						budget.record("reviewer", len(req.Objective)+len(req.Diff)+len(req.TestOutput)+500)
						saveCheckpoint(cfg.WorkspacePath(), req.TaskID, "reviewer", "review", resp.Summary)
					}
					return resp, err
				}
				fmt.Printf("✓ Reviewer agent using %s/%s\n", agentCfg.Provider, agentCfg.Model)
			} else {
				fmt.Printf("⚠ Reviewer agent not available: %v\n", err)
			}
			break
		}
	}

	// Build a role->provider+model map so /v1/agent/think can route to the
	// right model for each team role (lead plans, engineer implements, etc.).
	type roleEntry struct {
		provider providers.LLMProvider
		model    string
	}
	roleProviders := make(map[string]roleEntry)
	agentRoster := make([]orchestrator.AgentProfile, 0, len(cfg.Agents.List))
	for _, agentCfg := range cfg.Agents.List {
		if agentCfg.Role == "" || agentCfg.Provider == "" || agentCfg.Model == "" {
			continue
		}
		p, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
		if err != nil {
			fmt.Printf("⚠ Agent %s (%s): %v\n", agentCfg.ID, agentCfg.Role, err)
			continue
		}
		roleProviders[agentCfg.Role] = roleEntry{provider: p, model: agentCfg.Model}
		agentRoster = append(agentRoster, orchestrator.AgentProfile{
			ID:           agentCfg.ID,
			Name:         agentCfg.Name,
			Role:         agentCfg.Role,
			ProviderName: agentCfg.Provider,
			Model:        agentCfg.Model,
			Capabilities: agentCapabilitiesForRole(agentCfg.Role),
		})
		fmt.Printf("✓ %s agent (%s) using %s/%s\n", agentCfg.Role, agentCfg.ID, agentCfg.Provider, agentCfg.Model)
	}

	thinkFunc := func(ctx context.Context, req orchestrator.AgentThinkRequest) (orchestrator.AgentThinkResponse, error) {
		entry, ok := roleProviders[req.Role]
		if !ok {
			return orchestrator.AgentThinkResponse{}, fmt.Errorf("no agent configured for role %q", req.Role)
		}
		if err := budget.check(req.Role); err != nil {
			return orchestrator.AgentThinkResponse{}, err
		}

		// Per-action model override (MetaGPT pattern): let the orchestrator
		// select a cheaper model for simple actions within the same role.
		callProvider := entry.provider
		callModel := entry.model
		if req.ProviderName != "" && req.Model != "" {
			if p, err := providers.CreateProviderForFallback(cfg, req.ProviderName, req.Model); err == nil {
				callProvider = p
				callModel = req.Model
			}
		}

		messages := []providers.Message{{Role: "user", Content: req.Prompt}}
		resp, err := callProvider.Chat(ctx, messages, nil, callModel, map[string]interface{}{
			"max_tokens":  2048,
			"temperature": 0.3,
		})
		if err != nil {
			return orchestrator.AgentThinkResponse{}, err
		}
		if resp.Usage != nil {
			budget.record(req.Role, resp.Usage.TotalTokens)
		}
		saveCheckpoint(cfg.WorkspacePath(), req.TaskID, req.Role, "think", resp.Content[:min(len(resp.Content), 200)])
		return orchestrator.AgentThinkResponse{
			TaskID:  req.TaskID,
			Role:    req.Role,
			Content: resp.Content,
		}, nil
	}

	hostServer := orchestratorhost.NewServer(orchestratorhost.Config{
		SocketPath:          hostSocket,
		WorkspaceRoot:       cfg.WorkspacePath(),
		RestrictToWorkspace: cfg.Agents.Defaults.RestrictToWorkspace,
		Sandboxed:           cfg.Workspace.Sandboxed,
		TelegramEnabled:     cfg.Channels.Telegram.Enabled,
		ReviewChange:        reviewFunc,
		AgentThink:          thinkFunc,
		AgentRoster:         agentRoster,
	}, channelManager)
	go func() {
		if err := hostServer.Start(ctx); err != nil {
			logger.ErrorCF("orchestrator-host", "Host capability server error", map[string]interface{}{
				"error": err.Error(),
			})
		}
	}()
	fmt.Printf("✓ Orchestrator host server started on unix://%s\n", hostSocket)

	resumeIncompleteSessions(cfg.WorkspacePath())
	fmt.Printf("✓ Gateway started on %s:%d\n", cfg.Gateway.Host, cfg.Gateway.Port)
	fmt.Println("Press Ctrl+C to stop")

	if err := cronService.Start(); err != nil {
		fmt.Printf("Error starting cron service: %v\n", err)
	}
	fmt.Println("✓ Cron service started")

	if err := heartbeatService.Start(); err != nil {
		fmt.Printf("Error starting heartbeat service: %v\n", err)
	}
	fmt.Println("✓ Heartbeat service started")

	stateManager := state.NewManager(cfg.WorkspacePath())

	// V1 Event Router
	eventRouter := events.NewRouter()
	eventRouter.Start(ctx)
	fmt.Println("✓ V1 event router started")

	// V1 Job Queue
	jobQueue, err := queue.NewQueue(cfg.WorkspacePath())
	if err != nil {
		fmt.Printf("Error creating job queue: %v\n", err)
	} else {
		jobQueue.Start(ctx, 5*time.Second)
		fmt.Println("✓ V1 job queue started")
	}

	// Device Registry
	hostname, _ := os.Hostname()
	selfDevice := devsync.DeviceInfo{
		ID:       hostname,
		Name:     hostname,
		Host:     cfg.Gateway.Host,
		Port:     cfg.Gateway.Port,
		Platform: runtime.GOOS,
		Version:  version,
	}
	registry := devsync.NewRegistry(selfDevice)

	// Prune stale devices periodically.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if pruned := registry.PruneStale(3 * time.Minute); pruned > 0 {
					logger.InfoCF("sync", "Pruned stale devices", map[string]interface{}{"count": pruned})
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	fmt.Printf("✓ Device registry started (self: %s/%s)\n", selfDevice.ID, selfDevice.Platform)

	// V1 API Server
	var apiServer *api.Server
	if cfg.V1API.Enabled {
		apiServer = api.NewServer(api.Config{
			Addr:   cfg.V1API.Addr,
			APIKey: cfg.V1API.APIKey,
		}, msgBus, eventRouter, stateManager, registry)

		apiServer.SetChatHandler(func(ctx context.Context, message, sessionKey string) (string, error) {
			return agentLoop.ProcessDirectWithChannel(ctx, message, sessionKey, "api", sessionKey)
		})

		go func() {
			if err := apiServer.Start(ctx); err != nil {
				logger.ErrorCF("api", "V1 API server error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ V1 API server started on %s\n", cfg.V1API.Addr)
	}

	if err := channelManager.StartAll(ctx); err != nil {
		fmt.Printf("Error starting channels: %v\n", err)
	}

	healthServer := health.NewServer(cfg.Gateway.Host, cfg.Gateway.Port)
	go func() {
		if err := healthServer.Start(); err != nil && err != http.ErrServerClosed {
			logger.ErrorCF("health", "Health server error", map[string]interface{}{"error": err.Error()})
		}
	}()
	fmt.Printf("✓ Health endpoints available at http://%s:%d/health and /ready\n", cfg.Gateway.Host, cfg.Gateway.Port)

	if os.Getenv("VIKRAM_CONSOLE_ENABLED") == "1" {
		// Management dashboard with team, config, and task views. This remains
		// opt-in until the web control plane has production authentication.
		dashboardAddr := dashboardAddrFromEnv()
		consoleAddr := consoleAddrFromEnv()
		dashCfg := dashboard.Config{Enabled: true, Addr: dashboardAddr, Title: "Vikram"}
		dashSrv := dashboard.NewServer(dashCfg)
		dashSrv.SetStatusProvider(func() dashboard.StatusData {
			return dashboard.StatusData{
				Status:           "running",
				ActiveChannels:   channelManager.GetEnabledChannels(),
				TrackedUsers:     stateManager.UserCount(),
				WebSocketClients: 0,
			}
		})
		dashSrv.SetTeamProvider(func() []map[string]interface{} {
			var agents []map[string]interface{}
			for _, a := range cfg.Agents.List {
				agents = append(agents, map[string]interface{}{
					"id":                 a.ID,
					"role":               a.Role,
					"provider":           a.Provider,
					"model":              a.Model,
					"max_tokens_per_day": a.MaxTokensPerDay,
					"budget_action":      a.BudgetAction,
				})
			}
			return agents
		})
		dashSrv.SetConfigProvider(func() map[string]interface{} {
			providers := []map[string]interface{}{
				{"name": "deepseek", "configured": cfg.Providers.DeepSeek.APIKey != "", "base": cfg.Providers.DeepSeek.APIBase},
				{"name": "mistral", "configured": cfg.Providers.Mistral.APIKey != "", "base": cfg.Providers.Mistral.APIBase},
				{"name": "nvidia", "configured": cfg.Providers.Nvidia.APIKey != "", "base": cfg.Providers.Nvidia.APIBase},
				{"name": "zhipu", "configured": cfg.Providers.Zhipu.APIKey != "", "base": cfg.Providers.Zhipu.APIBase},
				{"name": "openai", "configured": cfg.Providers.OpenAI.APIKey != "", "base": cfg.Providers.OpenAI.APIBase},
				{"name": "anthropic", "configured": cfg.Providers.Anthropic.APIKey != "", "base": cfg.Providers.Anthropic.APIBase},
				{"name": "openrouter", "configured": cfg.Providers.OpenRouter.APIKey != "", "base": cfg.Providers.OpenRouter.APIBase},
				{"name": "vertex", "configured": cfg.Providers.Vertex.ProjectID != "", "base": cfg.Providers.Vertex.Location},
			}
			var agents []map[string]interface{}
			for _, a := range cfg.Agents.List {
				agents = append(agents, map[string]interface{}{
					"id":                 a.ID,
					"role":               a.Role,
					"provider":           a.Provider,
					"model":              a.Model,
					"max_tokens_per_day": a.MaxTokensPerDay,
				})
			}
			return map[string]interface{}{"providers": providers, "agents": agents}
		})
		go func() {
			if err := dashSrv.Start(ctx); err != nil && err != http.ErrServerClosed {
				logger.ErrorCF("dashboard", "Dashboard error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ Dashboard available at http://%s/manage\n", dashboardAddr)

		consoleSrv := console.NewServer(console.Config{Enabled: true, Addr: consoleAddr, APIKey: os.Getenv("VIKRAM_CONSOLE_API_KEY")}, cfg, getConfigPath())
		consoleSrv.SetOnAgentChange(func(action string, agentCfg config.AgentConfig) {
			if action == "add" {
				if agentCfg.Provider != "" && agentCfg.Model != "" {
					p, err := providers.CreateProviderForFallback(cfg, agentCfg.Provider, agentCfg.Model)
					if err == nil {
						agentLoop.SubagentManager.RegisterAgent(agentCfg.ID, agentCfg.Role, p, agentCfg.Model, nil, agentCfg.SystemPrompt)
						fmt.Printf("✓ Agent %s (%s) registered at runtime\n", agentCfg.ID, agentCfg.Role)
					} else {
						fmt.Printf("⚠ Agent %s provider failed: %v\n", agentCfg.ID, err)
					}
				}
			} else if action == "remove" {
				agentLoop.SubagentManager.UnregisterAgent(agentCfg.ID)
				fmt.Printf("✓ Agent %s removed at runtime\n", agentCfg.ID)
			}
		})
		consoleSrv.SetOnChannelChange(func(name string) {
			if err := channelManager.ReconnectChannel(ctx, name); err != nil {
				fmt.Printf("⚠ Channel %s reconnect failed: %v\n", name, err)
			} else {
				fmt.Printf("✓ Channel %s reconnected\n", name)
			}
		})
		consoleSrv.SetChatHandler(func(ctx context.Context, message string) (string, error) {
			msgBus.PublishInbound(bus.InboundMessage{
				Channel: "web", SenderID: "founder", ChatID: "web-console",
				Content: message, SessionKey: "web:web-console",
			})
			return "Queued for Vikram agent.", nil
		})
		go func() {
			sub := msgBus.SubscribeOutbound()
			defer sub.Unsubscribe()
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-sub.C:
					if !ok {
						return
					}
					if msg.Channel == "web" || msg.Channel == "" {
						consoleSrv.BroadcastChat("assistant", msg.Content)
					}
				}
			}
		}()
		go func() {
			if err := consoleSrv.Start(ctx); err != nil && err != http.ErrServerClosed {
				logger.ErrorCF("console", "Console error", map[string]interface{}{"error": err.Error()})
			}
		}()
		fmt.Printf("✓ Management console at http://%s\n", consoleAddr)
	}

	go agentLoop.Run(ctx)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nShutting down...")
	cancel()
	if apiServer != nil {
		apiServer.Stop()
	}
	_ = hostServer.Stop(context.Background())
	if jobQueue != nil {
		jobQueue.Stop()
	}
	eventRouter.Stop()
	healthServer.Stop(context.Background())
	heartbeatService.Stop()
	cronService.Stop()
	agentLoop.Stop()
	channelManager.StopAll(ctx)
	fmt.Println("✓ Gateway stopped")
}

func isPublicHost(host string) bool {
	// Normalise: lowercase and strip trailing DNS dot so "LOCALHOST.",
	// "Localhost", "::1" etc. are all treated correctly.
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" || host == "localhost" || host == "::1" {
		return false
	}
	if host == "0.0.0.0" || host == "::" || host == "[::]" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return true
	}
	return !ip.IsLoopback()
}

func enabledChannelsWithoutAllowlist(cfg *config.Config) []string {
	type channelRule struct {
		name      string
		enabled   bool
		allowList []string
	}

	rules := []channelRule{
		{name: "telegram", enabled: cfg.Channels.Telegram.Enabled, allowList: cfg.Channels.Telegram.AllowFrom},
		{name: "whatsapp", enabled: cfg.Channels.WhatsApp.Enabled, allowList: cfg.Channels.WhatsApp.AllowFrom},
	}

	var insecure []string
	for _, rule := range rules {
		if rule.enabled && len(rule.allowList) == 0 {
			insecure = append(insecure, rule.name)
		}
	}
	sort.Strings(insecure)
	return insecure
}

func validateGatewaySecurity(cfg *config.Config) error {
	if cfg.V1API.Enabled && strings.TrimSpace(cfg.V1API.APIKey) == "" {
		return fmt.Errorf("v1_api.api_key is required when v1_api.enabled=true")
	}

	if isPublicHost(cfg.Gateway.Host) {
		if !cfg.Agents.Defaults.RestrictToWorkspace {
			return fmt.Errorf("agents.defaults.restrict_to_workspace must be true for public gateway host %q", cfg.Gateway.Host)
		}

		// SEC-API-01: Enforce API key when the V1 API is exposed on a public host.
		if cfg.V1API.Enabled && strings.TrimSpace(cfg.V1API.APIKey) == "" {
			return fmt.Errorf("v1_api.api_key is required when gateway is on public host %q", cfg.Gateway.Host)
		}

		if insecureChannels := enabledChannelsWithoutAllowlist(cfg); len(insecureChannels) > 0 {
			return fmt.Errorf("public gateway host %q requires allow_from for enabled channels: %s", cfg.Gateway.Host, strings.Join(insecureChannels, ", "))
		}
	}

	return nil
}

func orchestratorHostSocketPath() string {
	if socketPath := strings.TrimSpace(os.Getenv("VIKRAM_HOST_SOCKET")); socketPath != "" {
		return socketPath
	}
	// Security: Use a user-private directory instead of world-writable /tmp
	// to prevent symlink attacks, socket hijacking, and unauthorized access.
	runDir := filepath.Join(config.HomeDir(), "run")
	if err := os.MkdirAll(runDir, 0700); err != nil {
		// Fallback to a user-namespaced path in /tmp if home dir is unavailable
		return fmt.Sprintf("/tmp/vikramd-%d.sock", os.Getuid())
	}
	return filepath.Join(runDir, "vikramd.sock")
}


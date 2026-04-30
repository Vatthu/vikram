package console

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/v1claw/levik/pkg/config"
)

// --- Dashboard ---

func (s *Server) handleAPIDashboard(w http.ResponseWriter, r *http.Request) {
	budget := s.cfg.WorkspacePath()
	s.writeOK(w, map[string]interface{}{
		"status":            "running",
		"agent_count":       len(s.cfg.Agents.List),
		"provider_count":    countConfiguredProviders(s.cfg),
		"gateway_host":      s.cfg.Gateway.Host,
		"gateway_port":      s.cfg.Gateway.Port,
		"workspace":         budget,
		"sandboxed":         s.cfg.Workspace.Sandboxed,
		"telegram_enabled":  s.cfg.Channels.Telegram.Enabled,
		"whatsapp_enabled":  s.cfg.Channels.WhatsApp.Enabled,
		"heartbeat_enabled": s.cfg.Heartbeat.Enabled,
		"mcp_enabled":       s.cfg.MCP.Enabled,
		"ws_clients":        len(s.hub.clients),
	})
}

func countConfiguredProviders(cfg *config.Config) int {
	n := 0
	for _, p := range []*config.ProviderConfig{
		&cfg.Providers.DeepSeek, &cfg.Providers.Mistral, &cfg.Providers.Nvidia,
		&cfg.Providers.Zhipu, &cfg.Providers.OpenAI, &cfg.Providers.Anthropic,
		&cfg.Providers.OpenRouter, &cfg.Providers.Gemini, &cfg.Providers.Groq,
		&cfg.Providers.Cerebras, &cfg.Providers.SambaNova, &cfg.Providers.GitHubModels,
		&cfg.Providers.Moonshot, &cfg.Providers.XAI,
	} {
		if p.APIKey != "" {
			n++
		}
	}
	if cfg.Providers.Vertex.ProjectID != "" {
		n++
	}
	if cfg.Providers.Ollama.APIBase != "" {
		n++
	}
	if cfg.Providers.AzureOpenAI.APIKey != "" {
		n++
	}
	if cfg.Providers.Bedrock.Region != "" {
		n++
	}
	if cfg.Providers.GitHubCopilot.APIKey != "" {
		n++
	}
	if cfg.Providers.VLLM.APIBase != "" {
		n++
	}
	return n
}

// --- Channels ---

func (s *Server) handleAPIChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		// POST to /api/channels/{name} — save channel config
		name := strings.TrimPrefix(r.URL.Path, "/api/channels/")
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		switch name {
		case "telegram":
			if v, ok := body["token"].(string); ok {
				s.cfg.Channels.Telegram.Token = v
			}
			if v, ok := body["enabled"].(bool); ok {
				s.cfg.Channels.Telegram.Enabled = v
			}
			if v, ok := body["proxy"].(string); ok {
				s.cfg.Channels.Telegram.Proxy = v
			}
		case "whatsapp":
			if v, ok := body["enabled"].(bool); ok {
				s.cfg.Channels.WhatsApp.Enabled = v
			}
			if v, ok := body["bridge_url"].(string); ok {
				s.cfg.Channels.WhatsApp.BridgeURL = v
			}
			if v, ok := body["token"].(string); ok {
				s.cfg.Channels.WhatsApp.BridgeToken = v
			}
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.hub.Broadcast("channel_updated", map[string]string{"name": name})
	}
	// GET: return channel status
	s.writeOK(w, map[string]interface{}{
		"telegram": map[string]interface{}{
			"enabled":    s.cfg.Channels.Telegram.Enabled,
			"token":      maskKey(s.cfg.Channels.Telegram.Token),
			"proxy":      s.cfg.Channels.Telegram.Proxy,
			"allow_from": s.cfg.Channels.Telegram.AllowFrom,
		},
		"whatsapp": map[string]interface{}{
			"enabled":    s.cfg.Channels.WhatsApp.Enabled,
			"bridge_url": s.cfg.Channels.WhatsApp.BridgeURL,
			"token":      maskKey(s.cfg.Channels.WhatsApp.BridgeToken),
			"allow_from": s.cfg.Channels.WhatsApp.AllowFrom,
		},
	})
}

// --- Gateway ---

func (s *Server) handleAPIGateway(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, map[string]interface{}{
			"host": s.cfg.Gateway.Host, "port": s.cfg.Gateway.Port,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["host"].(string); ok {
			s.cfg.Gateway.Host = v
		}
		if v, ok := body["port"].(float64); ok {
			s.cfg.Gateway.Port = int(v)
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "saved"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- Workspace ---

func (s *Server) handleAPIWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, map[string]interface{}{
			"path": s.cfg.Workspace.Path, "sandboxed": s.cfg.Workspace.Sandboxed,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["path"].(string); ok {
			s.cfg.Workspace.Path = v
		}
		if v, ok := body["sandboxed"].(bool); ok {
			s.cfg.Workspace.Sandboxed = v
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "saved"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- Tools ---

func (s *Server) handleAPITools(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, map[string]interface{}{
			"web": map[string]interface{}{
				"duckduckgo": map[string]interface{}{"enabled": s.cfg.Tools.Web.DuckDuckGo.Enabled, "max_results": s.cfg.Tools.Web.DuckDuckGo.MaxResults},
				"brave":      map[string]interface{}{"enabled": s.cfg.Tools.Web.Brave.Enabled, "max_results": s.cfg.Tools.Web.Brave.MaxResults},
				"perplexity": map[string]interface{}{"enabled": s.cfg.Tools.Web.Perplexity.Enabled, "max_results": s.cfg.Tools.Web.Perplexity.MaxResults},
			},
			"cron": map[string]interface{}{"exec_timeout_minutes": s.cfg.Tools.Cron.ExecTimeoutMinutes},
		})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if d, ok := body["duckduckgo_enabled"].(bool); ok {
			s.cfg.Tools.Web.DuckDuckGo.Enabled = d
		}
		if b, ok := body["brave_enabled"].(bool); ok {
			s.cfg.Tools.Web.Brave.Enabled = b
		}
		if p, ok := body["perplexity_enabled"].(bool); ok {
			s.cfg.Tools.Web.Perplexity.Enabled = p
		}
		if v, ok := body["cron_timeout"].(float64); ok {
			s.cfg.Tools.Cron.ExecTimeoutMinutes = int(v)
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "saved"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- MCP ---

func (s *Server) handleAPIMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		servers := s.cfg.MCP.Servers
		if servers == nil {
			servers = []config.MCPServerConfig{}
		}
		s.writeOK(w, map[string]interface{}{
			"enabled": s.cfg.MCP.Enabled,
			"servers": servers,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body config.MCPServerConfig
		json.NewDecoder(r.Body).Decode(&body)
		s.cfg.MCP.Servers = append(s.cfg.MCP.Servers, body)
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "added"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

func (s *Server) handleAPIMCPDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var filtered []config.MCPServerConfig
	for _, srv := range s.cfg.MCP.Servers {
		if srv.Name != name {
			filtered = append(filtered, srv)
		}
	}
	s.cfg.MCP.Servers = filtered
	config.SaveConfig(s.cfgPath, s.cfg)
	s.writeOK(w, map[string]string{"status": "removed"})
}

// --- Voice ---

func (s *Server) handleAPIVoice(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, map[string]interface{}{
			"enabled": s.cfg.Voice.Enabled, "mode": s.cfg.Voice.Mode,
			"tts_provider": s.cfg.Voice.TTSProvider, "wake_words": s.cfg.Voice.WakeWordPhrases,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["enabled"].(bool); ok {
			s.cfg.Voice.Enabled = v
		}
		if v, ok := body["mode"].(string); ok {
			s.cfg.Voice.Mode = v
		}
		if v, ok := body["tts_provider"].(string); ok {
			s.cfg.Voice.TTSProvider = v
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "saved"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- Heartbeat ---

func (s *Server) handleAPIHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, map[string]interface{}{
			"enabled": s.cfg.Heartbeat.Enabled, "interval_minutes": s.cfg.Heartbeat.Interval,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["enabled"].(bool); ok {
			s.cfg.Heartbeat.Enabled = v
		}
		if v, ok := body["interval"].(float64); ok {
			s.cfg.Heartbeat.Interval = int(v)
		}
		config.SaveConfig(s.cfgPath, s.cfg)
		s.writeOK(w, map[string]string{"status": "saved"})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- Tasks ---

func (s *Server) handleAPITasks(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		tasks := []map[string]interface{}{}
		if err := s.orchestratorJSON(ctx, http.MethodGet, "/v1/tasks", nil, &tasks); err != nil {
			s.writeOK(w, map[string]interface{}{
				"tasks":                  tasks,
				"note":                   "Orchestrator is not reachable at " + s.orchSocket + ". Start it to load real tasks.",
				"orchestrator_reachable": false,
			})
			return
		}

		note := "No tasks yet. Submit a task to get started."
		if len(tasks) > 0 {
			note = ""
		}
		s.writeOK(w, map[string]interface{}{
			"tasks":                  tasks,
			"note":                   note,
			"orchestrator_reachable": true,
		})
		return
	}
	if r.Method == http.MethodPost {
		var body consoleTaskSubmission
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid task request")
			return
		}

		request, err := body.toOrchestratorRequest()
		if err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
		defer cancel()

		var task map[string]interface{}
		if err := s.orchestratorJSON(ctx, http.MethodPost, "/v1/tasks", request, &task); err != nil {
			status := http.StatusServiceUnavailable
			if isOrchestratorHTTPError(err) {
				status = http.StatusBadGateway
			}
			s.writeError(w, status, err.Error())
			return
		}

		s.hub.Broadcast("task_submitted", task)
		s.writeOK(w, map[string]interface{}{
			"status":  "submitted",
			"task_id": request.TaskID,
			"task":    task,
		})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

type consoleTaskSubmission struct {
	TaskID               string            `json:"task_id"`
	Objective            string            `json:"objective"`
	RepoPath             string            `json:"repo_path"`
	Repo                 consoleTaskRepo   `json:"repo"`
	DefaultBranch        string            `json:"default_branch"`
	RequireHumanApproval *bool             `json:"require_human_approval"`
	MaxParallelWorkers   int               `json:"max_parallel_workers"`
	MaxCostUSD           *float64          `json:"max_cost_usd"`
	AllowNetwork         bool              `json:"allow_network"`
	OperatorChannel      string            `json:"operator_channel"`
	OperatorChatID       string            `json:"operator_chat_id"`
	Metadata             map[string]string `json:"metadata,omitempty"`
}

type consoleTaskRepo struct {
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
}

type orchestratorTaskRequest struct {
	TaskID          string                      `json:"task_id"`
	Source          string                      `json:"source"`
	RequestedBy     string                      `json:"requested_by"`
	Objective       string                      `json:"objective"`
	Repo            consoleTaskRepo             `json:"repo"`
	Constraints     orchestratorTaskConstraints `json:"constraints"`
	OperatorChannel string                      `json:"operator_channel,omitempty"`
	OperatorChatID  string                      `json:"operator_chat_id,omitempty"`
}

type orchestratorTaskConstraints struct {
	RequireHumanApproval bool     `json:"require_human_approval"`
	MaxParallelWorkers   int      `json:"max_parallel_workers"`
	MaxCostUSD           *float64 `json:"max_cost_usd,omitempty"`
	AllowNetwork         bool     `json:"allow_network"`
}

func (body consoleTaskSubmission) toOrchestratorRequest() (orchestratorTaskRequest, error) {
	objective := strings.TrimSpace(body.Objective)
	if objective == "" {
		return orchestratorTaskRequest{}, fmt.Errorf("objective is required")
	}

	repoPath := strings.TrimSpace(body.RepoPath)
	if repoPath == "" {
		repoPath = strings.TrimSpace(body.Repo.Path)
	}
	if repoPath == "" {
		return orchestratorTaskRequest{}, fmt.Errorf("repo_path is required")
	}

	defaultBranch := strings.TrimSpace(body.DefaultBranch)
	if defaultBranch == "" {
		defaultBranch = strings.TrimSpace(body.Repo.DefaultBranch)
	}
	if defaultBranch == "" {
		defaultBranch = "main"
	}

	taskID := strings.TrimSpace(body.TaskID)
	if taskID == "" {
		taskID = fmt.Sprintf("task-%d", time.Now().UnixMilli())
	}

	requireHumanApproval := true
	if body.RequireHumanApproval != nil {
		requireHumanApproval = *body.RequireHumanApproval
	}

	maxParallelWorkers := body.MaxParallelWorkers
	if maxParallelWorkers <= 0 {
		maxParallelWorkers = 1
	}

	return orchestratorTaskRequest{
		TaskID:      taskID,
		Source:      "console",
		RequestedBy: "founder",
		Objective:   objective,
		Repo: consoleTaskRepo{
			Path:          repoPath,
			DefaultBranch: defaultBranch,
		},
		Constraints: orchestratorTaskConstraints{
			RequireHumanApproval: requireHumanApproval,
			MaxParallelWorkers:   maxParallelWorkers,
			MaxCostUSD:           body.MaxCostUSD,
			AllowNetwork:         body.AllowNetwork,
		},
		OperatorChannel: strings.TrimSpace(body.OperatorChannel),
		OperatorChatID:  strings.TrimSpace(body.OperatorChatID),
	}, nil
}

// --- Skills ---

func (s *Server) handleAPISkills(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Skills are loaded by the skills loader at gateway startup.
		// For now, return available skills from the workspace.
		s.writeOK(w, map[string]interface{}{
			"skills": []map[string]string{},
			"note":   "Skills loaded from workspace/skills/ and global skills directory.",
		})
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "")
}

// --- Full Config ---

func (s *Server) handleAPIFullConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		s.writeOK(w, s.cfg)
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "GET only")
}

// --- Health ---

func (s *Server) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	s.writeOK(w, map[string]interface{}{
		"status":  "ok",
		"uptime":  "running",
		"version": "v1",
	})
}

// parseBool is a small helper used by form parsing (legacy routes).
func parseBool(s string) bool {
	b, _ := strconv.ParseBool(s)
	return b
}

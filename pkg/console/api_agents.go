package console

import (
	"encoding/json"
	"net/http"

	"github.com/Vatthu/vikram/pkg/config"
)

func (s *Server) handleAPIAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		agents := make([]map[string]interface{}, len(s.cfg.Agents.List))
		for i, a := range s.cfg.Agents.List {
			agents[i] = agentToJSON(a)
		}
		s.writeOK(w, agents)
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handleAPIAgentByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if r.Method == http.MethodGet {
		for _, a := range s.cfg.Agents.List {
			if a.ID == id {
				s.writeOK(w, agentToJSON(a))
				return
			}
		}
		s.writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if r.Method == http.MethodPut {
		var body map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		for i, a := range s.cfg.Agents.List {
			if a.ID == id {
				if v, ok := body["role"].(string); ok {
					s.cfg.Agents.List[i].Role = v
				}
				if v, ok := body["provider"].(string); ok {
					s.cfg.Agents.List[i].Provider = v
				}
				if v, ok := body["model"].(string); ok {
					s.cfg.Agents.List[i].Model = v
				}
				if v, ok := body["system_prompt"].(string); ok {
					s.cfg.Agents.List[i].SystemPrompt = v
				}
				if v, ok := body["max_tokens_per_day"].(float64); ok {
					s.cfg.Agents.List[i].MaxTokensPerDay = int(v)
				}
				if v, ok := body["budget_action"].(string); ok {
					s.cfg.Agents.List[i].BudgetAction = v
				}
				config.SaveConfig(s.cfgPath, s.cfg)
				s.hub.Broadcast("agent_updated", map[string]string{"id": id})
				s.writeOK(w, agentToJSON(s.cfg.Agents.List[i]))
				return
			}
		}
		s.writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handleAPIAddAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	var body struct {
		ID, Name, Role, Provider, Model, SystemPrompt, BudgetAction string
		MaxTokensPerDay                                             int
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" || body.Role == "" {
		s.writeError(w, http.StatusBadRequest, "id and role are required")
		return
	}
	agent := config.AgentConfig{
		ID: body.ID, Name: body.Name, Role: body.Role,
		Provider: body.Provider, Model: body.Model,
		SystemPrompt: body.SystemPrompt, MaxTokensPerDay: body.MaxTokensPerDay,
		BudgetAction: body.BudgetAction,
	}
	s.cfg.Agents.List = append(s.cfg.Agents.List, agent)
	config.SaveConfig(s.cfgPath, s.cfg)
	if s.onAgentChange != nil {
		s.onAgentChange("add", agent)
	}
	s.hub.Broadcast("agent_added", map[string]string{"id": body.ID, "role": body.Role})
	s.writeOK(w, agentToJSON(agent))
}

func (s *Server) handleAPIRemoveAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	id := r.PathValue("id")
	var removed config.AgentConfig
	var filtered []config.AgentConfig
	for _, a := range s.cfg.Agents.List {
		if a.ID != id {
			filtered = append(filtered, a)
		} else {
			removed = a
		}
	}
	s.cfg.Agents.List = filtered
	config.SaveConfig(s.cfgPath, s.cfg)
	if s.onAgentChange != nil && removed.ID != "" {
		s.onAgentChange("remove", removed)
	}
	s.hub.Broadcast("agent_removed", map[string]string{"id": id})
	s.writeOK(w, map[string]string{"status": "removed", "id": id})
}

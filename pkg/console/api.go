package console

import (
	"encoding/json"
	"net/http"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/logger"
)

// --- JSON helpers ---

func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.ErrorCF("console", "JSON encode failed", map[string]interface{}{"error": err.Error()})
	}
}

func (s *Server) writeOK(w http.ResponseWriter, data interface{}) {
	s.writeJSON(w, http.StatusOK, data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]string{"error": msg})
}

func maskKey(k string) string {
	if len(k) > 8 {
		return k[:4] + "..." + k[len(k)-4:]
	}
	if k != "" {
		return "****"
	}
	return ""
}

func providerStatus(cfg *config.ProviderConfig) string {
	if cfg.APIKey != "" {
		return "configured"
	}
	return "not set"
}

// agentToJSON converts an AgentConfig to a JSON-serializable map.
func agentToJSON(a config.AgentConfig) map[string]interface{} {
	return map[string]interface{}{
		"id":                 a.ID,
		"name":               a.Name,
		"role":               a.Role,
		"provider":           a.Provider,
		"model":              a.Model,
		"workspace":          a.Workspace,
		"system_prompt":      a.SystemPrompt,
		"max_tokens_per_day": a.MaxTokensPerDay,
		"budget_action":      a.BudgetAction,
	}
}

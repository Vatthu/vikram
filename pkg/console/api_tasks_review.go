package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"time"

	"github.com/Vatthu/vikram/pkg/orchestrator"
)

func (s *Server) handleAPITaskReview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}

	taskID := r.PathValue("task_id")
	if taskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var review map[string]interface{}
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/review"
	if err := s.orchestratorJSON(ctx, http.MethodGet, path, nil, &review); err != nil {
		s.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.writeOK(w, review)
}

func (s *Server) handleAPITaskResume(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}

	taskID := r.PathValue("task_id")
	if taskID == "" {
		s.writeError(w, http.StatusBadRequest, "task_id is required")
		return
	}

	var decision orchestrator.ApprovalDecision
	if err := json.NewDecoder(r.Body).Decode(&decision); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if decision.TaskID != taskID {
		s.writeError(w, http.StatusBadRequest, "task_id mismatch")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()

	var session orchestrator.TaskSession
	path := "/v1/tasks/" + url.PathEscape(taskID) + "/resume"
	if err := s.orchestratorJSON(ctx, http.MethodPost, path, decision, &session); err != nil {
		s.writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.writeOK(w, session)
}

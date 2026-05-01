package console

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/v1claw/levik/pkg/config"
	"github.com/v1claw/levik/pkg/orchestrator"
	"github.com/v1claw/levik/pkg/pairing"
)

func TestHandleAPITasksListsRealOrchestratorTasks(t *testing.T) {
	server := testConsoleServer(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/tasks" {
			return nil, unexpectedRequestError(r)
		}
		return testJSONResponse(t, http.StatusOK, []map[string]interface{}{
			{
				"task_id":    "task-001",
				"objective":  "Wire the console",
				"phase":      "change_ready",
				"status":     "running",
				"risk_class": "low",
			},
		}), nil
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)

	server.handleAPITasks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Tasks                 []map[string]interface{} `json:"tasks"`
		Note                  string                   `json:"note"`
		OrchestratorReachable bool                     `json:"orchestrator_reachable"`
	}
	decodeTestJSON(t, recorder, &payload)

	if !payload.OrchestratorReachable {
		t.Fatal("expected orchestrator_reachable=true")
	}
	if payload.Note != "" {
		t.Fatalf("expected empty note, got %q", payload.Note)
	}
	if len(payload.Tasks) != 1 || payload.Tasks[0]["task_id"] != "task-001" {
		t.Fatalf("unexpected tasks payload: %#v", payload.Tasks)
	}
}

func TestHandleAPITasksReturnsEmptyStateWhenOrchestratorIsDown(t *testing.T) {
	server := testConsoleServer(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)

	server.handleAPITasks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Tasks                 []map[string]interface{} `json:"tasks"`
		Note                  string                   `json:"note"`
		OrchestratorReachable bool                     `json:"orchestrator_reachable"`
	}
	decodeTestJSON(t, recorder, &payload)

	if payload.OrchestratorReachable {
		t.Fatal("expected orchestrator_reachable=false")
	}
	if len(payload.Tasks) != 0 {
		t.Fatalf("expected no tasks, got %#v", payload.Tasks)
	}
	if payload.Note == "" {
		t.Fatal("expected an offline note")
	}
}

func TestHandleAPITasksSubmitsFounderTaskToOrchestrator(t *testing.T) {
	var upstreamRequest orchestratorTaskRequest
	server := testConsoleServer(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tasks" {
			return nil, unexpectedRequestError(r)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstreamRequest); err != nil {
			return nil, err
		}
		return testJSONResponse(t, http.StatusOK, map[string]interface{}{
			"task_id":    upstreamRequest.TaskID,
			"source":     upstreamRequest.Source,
			"objective":  upstreamRequest.Objective,
			"phase":      "change_ready",
			"status":     "running",
			"risk_class": "low",
			"repo": map[string]string{
				"path":           upstreamRequest.Repo.Path,
				"default_branch": upstreamRequest.Repo.DefaultBranch,
			},
		}), nil
	}))

	body := bytes.NewBufferString(`{
		"task_id":"task-console-001",
		"objective":"Run a real task through LeVik",
		"repo_path":"/repos/levik",
		"default_branch":"main",
		"allow_network":true
	}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/tasks", body)

	server.handleAPITasks(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if upstreamRequest.TaskID != "task-console-001" {
		t.Fatalf("unexpected task_id: %q", upstreamRequest.TaskID)
	}
	if upstreamRequest.Source != "console" || upstreamRequest.RequestedBy != "founder" {
		t.Fatalf("unexpected source/requested_by: %#v", upstreamRequest)
	}
	if upstreamRequest.Repo.Path != "/repos/levik" || upstreamRequest.Repo.DefaultBranch != "main" {
		t.Fatalf("unexpected repo: %#v", upstreamRequest.Repo)
	}
	if !upstreamRequest.Constraints.RequireHumanApproval {
		t.Fatal("expected human approval to default on")
	}
	if upstreamRequest.Constraints.MaxParallelWorkers != 1 {
		t.Fatalf("unexpected max_parallel_workers: %d", upstreamRequest.Constraints.MaxParallelWorkers)
	}
	if !upstreamRequest.Constraints.AllowNetwork {
		t.Fatal("expected allow_network to pass through")
	}

	var payload struct {
		Status string                 `json:"status"`
		TaskID string                 `json:"task_id"`
		Task   map[string]interface{} `json:"task"`
	}
	decodeTestJSON(t, recorder, &payload)
	if payload.Status != "submitted" || payload.TaskID != "task-console-001" {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestHandleAPITasksRejectsMissingRepoPath(t *testing.T) {
	server := testConsoleServer(nil)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/tasks",
		bytes.NewBufferString(`{"objective":"No target repo"}`),
	)

	server.handleAPITasks(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandleAPITaskReviewProxiesReviewDetail(t *testing.T) {
	server := testConsoleServer(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/tasks/task-001/review" {
			return nil, unexpectedRequestError(r)
		}
		return testJSONResponse(t, http.StatusOK, map[string]interface{}{
			"task": map[string]interface{}{
				"task_id":   "task-001",
				"objective": "Wire the console",
				"phase":     "awaiting_approval",
				"status":    "awaiting_approval",
			},
			"can_resume": true,
		}), nil
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/tasks/task-001/review", nil)
	request.SetPathValue("task_id", "task-001")

	server.handleAPITaskReview(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}

	var payload map[string]interface{}
	decodeTestJSON(t, recorder, &payload)
	if payload["can_resume"] != true {
		t.Fatalf("unexpected review payload: %#v", payload)
	}
}

func TestHandleAPITaskResumeProxiesDecision(t *testing.T) {
	var upstream orchestrator.ApprovalDecision
	server := testConsoleServer(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tasks/task-001/resume" {
			return nil, unexpectedRequestError(r)
		}
		if err := json.NewDecoder(r.Body).Decode(&upstream); err != nil {
			return nil, err
		}
		return testJSONResponse(t, http.StatusOK, map[string]interface{}{
			"task_id":   "task-001",
			"objective": "Wire the console",
			"phase":     "founder_approved",
			"status":    "completed",
		}), nil
	}))

	recorder := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"task_id":"task-001","decision":"approve","comment":"ship it"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/tasks/task-001/resume", body)
	request.SetPathValue("task_id", "task-001")

	server.handleAPITaskResume(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if upstream.TaskID != "task-001" || upstream.Decision != orchestrator.ApprovalDecisionApprove {
		t.Fatalf("unexpected upstream decision: %#v", upstream)
	}
	if upstream.Comment != "ship it" {
		t.Fatalf("unexpected upstream comment: %#v", upstream)
	}
}

func TestHandleTelegramPairApprovesOtpAndTriggersReconnect(t *testing.T) {
	home := t.TempDir()
	cfg := config.DefaultConfig()
	cfgPath := filepath.Join(home, "config.json")
	if err := config.SaveConfig(cfgPath, cfg); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	req, err := pairing.NewTelegramStoreAt(home).CreateOrReuse("123|alice", "123", "alice", "Alice")
	if err != nil {
		t.Fatalf("CreateOrReuse() error = %v", err)
	}

	changed := make(chan string, 1)
	server := &Server{
		hub:     newWSHub(),
		cfg:     cfg,
		cfgPath: cfgPath,
	}
	server.SetOnChannelChange(func(name string) {
		changed <- name
	})

	recorder := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"otp":"` + req.OTP + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/telegram/pair", body)

	server.handleTelegramPair(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(cfg.Channels.Telegram.AllowFrom) != 1 || cfg.Channels.Telegram.AllowFrom[0] != "123|alice" {
		t.Fatalf("unexpected allowlist: %#v", cfg.Channels.Telegram.AllowFrom)
	}
	select {
	case name := <-changed:
		if name != "telegram" {
			t.Fatalf("unexpected channel change: %q", name)
		}
	case <-time.After(time.Second):
		t.Fatal("expected telegram reconnect callback")
	}
}

func testConsoleServer(transport http.RoundTripper) *Server {
	socketPath := "missing-orchestrator.sock"
	var client *http.Client
	var baseURL string
	if transport != nil {
		client = &http.Client{Transport: transport}
		baseURL = "http://levik-orchestrator.test"
		socketPath = "test-orchestrator.sock"
	}
	return &Server{
		hub:            newWSHub(),
		orchSocket:     socketPath,
		orchHTTPClient: client,
		orchBaseURL:    baseURL,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}

func unexpectedRequestError(r *http.Request) error {
	return &badRequestError{method: r.Method, path: r.URL.Path}
}

type badRequestError struct {
	method string
	path   string
}

func (e *badRequestError) Error() string {
	return "unexpected request " + e.method + " " + e.path
}

func testJSONResponse(t *testing.T, status int, payload interface{}) *http.Response {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		t.Fatalf("encode json response: %v", err)
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(&body),
	}
}

func decodeTestJSON(t *testing.T, recorder *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.NewDecoder(recorder.Body).Decode(out); err != nil {
		t.Fatalf("decode response json: %v; body=%s", err, recorder.Body.String())
	}
}

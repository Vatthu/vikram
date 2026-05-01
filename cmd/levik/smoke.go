package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/v1claw/levik/pkg/config"
)

const defaultSmokeTimeout = 90 * time.Second

type smokeOptions struct {
	keepTemp bool
	timeout  time.Duration
}

type smokeProcess struct {
	cmd     *exec.Cmd
	logPath string
	done    chan error
}

type smokeConsoleHealth struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

type smokeConsoleOrchestratorStatus struct {
	Running   bool   `json:"running"`
	Reachable bool   `json:"reachable"`
	Socket    string `json:"socket"`
}

type smokeConsoleTask struct {
	TaskID         string `json:"task_id"`
	Objective      string `json:"objective"`
	Phase          string `json:"phase"`
	Status         string `json:"status"`
	RiskClass      string `json:"risk_class"`
	MergeReadiness string `json:"merge_readiness"`
}

type smokeConsoleTaskList struct {
	Tasks                 []smokeConsoleTask `json:"tasks"`
	Note                  string             `json:"note"`
	OrchestratorReachable bool               `json:"orchestrator_reachable"`
}

type smokeConsoleTaskSubmitResponse struct {
	Status string           `json:"status"`
	TaskID string           `json:"task_id"`
	Task   smokeTaskSession `json:"task"`
}

type smokeTaskSession struct {
	TaskID         string `json:"task_id"`
	Objective      string `json:"objective"`
	Status         string `json:"status"`
	Phase          string `json:"phase"`
	Summary        string `json:"summary"`
	RiskClass      string `json:"risk_class"`
	ApprovalRoute  string `json:"approval_route"`
	MergeReadiness string `json:"merge_readiness"`
	MergeSummary   string `json:"merge_summary"`
}

type smokeTextReplacement struct {
	Path      string `json:"path"`
	OldText   string `json:"old_text"`
	NewText   string `json:"new_text"`
	Rationale string `json:"rationale"`
}

type smokeTaskChangeRequest struct {
	TaskID               string                 `json:"task_id"`
	Summary              string                 `json:"summary"`
	Edits                []smokeTextReplacement `json:"edits"`
	VerificationCommands []string               `json:"verification_commands,omitempty"`
}

type smokeTaskResumeRequest struct {
	TaskID        string                 `json:"task_id"`
	Decision      string                 `json:"decision"`
	Comment       string                 `json:"comment"`
	ProposedEdits map[string]interface{} `json:"proposed_edits"`
}

type smokeTaskReview struct {
	Task                           smokeTaskSession `json:"task"`
	ChangeArtifactPath             string           `json:"change_artifact_path"`
	VerificationResultArtifactPath string           `json:"verification_result_artifact_path"`
	MergeArtifactPath              string           `json:"merge_artifact_path"`
	ArtifactPreviews               []struct {
		Title string `json:"title"`
		Kind  string `json:"kind"`
		Path  string `json:"path"`
	} `json:"artifact_previews"`
}

type smokeArtifactContent struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	BytesRead int    `json:"bytes_read"`
	Truncated bool   `json:"truncated"`
}

func smokeCmd() {
	target := "orchestrator"
	args := os.Args[2:]
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		target = strings.TrimSpace(args[0])
		args = args[1:]
	}

	opts, err := parseSmokeOptions(args)
	if err != nil {
		fmt.Printf("Smoke configuration error: %v\n", err)
		os.Exit(1)
	}

	switch target {
	case "", "orchestrator":
		if err := runOrchestratorSmoke(opts); err != nil {
			fmt.Printf("Orchestrator smoke failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown smoke target: %s\n", target)
		fmt.Println("Usage: levik smoke [orchestrator] [--keep-temp] [--timeout 90s]")
		os.Exit(1)
	}
}

func parseSmokeOptions(args []string) (smokeOptions, error) {
	opts := smokeOptions{timeout: defaultSmokeTimeout}

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--keep-temp":
			opts.keepTemp = true
		case "--timeout":
			i++
			if i >= len(args) {
				return smokeOptions{}, fmt.Errorf("--timeout requires a duration")
			}
			duration, err := time.ParseDuration(strings.TrimSpace(args[i]))
			if err != nil {
				return smokeOptions{}, fmt.Errorf("invalid --timeout value: %w", err)
			}
			if duration <= 0 {
				return smokeOptions{}, fmt.Errorf("--timeout must be greater than zero")
			}
			opts.timeout = duration
		default:
			return smokeOptions{}, fmt.Errorf("unknown option %q", args[i])
		}
	}

	return opts, nil
}

func runOrchestratorSmoke(opts smokeOptions) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	projectRoot, err := detectProjectRoot()
	if err != nil {
		return err
	}

	venvPython := filepath.Join(projectRoot, "services", "orchestrator", ".venv", "bin", "python")
	if _, err := os.Stat(venvPython); err != nil {
		return fmt.Errorf("orchestrator venv is not ready at %s", venvPython)
	}

	tmpRoot, err := os.MkdirTemp("", "levik-smoke-*")
	if err != nil {
		return fmt.Errorf("create smoke workspace: %w", err)
	}
	cleanupTemp := !opts.keepTemp
	defer func() {
		if cleanupTemp {
			_ = os.RemoveAll(tmpRoot)
		}
	}()

	homeDir := filepath.Join(tmpRoot, "home")
	workspaceDir := filepath.Join(tmpRoot, "workspace")
	repoDir := smokeRepoPath(workspaceDir)
	runDir := filepath.Join(tmpRoot, "run")
	logDir := filepath.Join(tmpRoot, "logs")
	for _, dir := range []string{homeDir, workspaceDir, repoDir, runDir, logDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}

	if err := createSmokeRepo(ctx, repoDir); err != nil {
		return attachSmokeContext(err, tmpRoot, logDir)
	}

	stubURL, stopStub, err := startSmokeLLMStub()
	if err != nil {
		return attachSmokeContext(fmt.Errorf("start local stub: %w", err), tmpRoot, logDir)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = stopStub(shutdownCtx)
	}()

	healthPort, err := pickFreeTCPPort()
	if err != nil {
		return attachSmokeContext(fmt.Errorf("allocate health port: %w", err), tmpRoot, logDir)
	}
	consolePort, err := pickFreeTCPPort()
	if err != nil {
		return attachSmokeContext(fmt.Errorf("allocate console port: %w", err), tmpRoot, logDir)
	}
	dashboardPort, err := pickFreeTCPPort()
	if err != nil {
		return attachSmokeContext(fmt.Errorf("allocate dashboard port: %w", err), tmpRoot, logDir)
	}

	cfg := buildSmokeConfig(workspaceDir, stubURL, healthPort)
	if err := config.SaveConfig(filepath.Join(homeDir, "config.json"), cfg); err != nil {
		return attachSmokeContext(fmt.Errorf("write smoke config: %w", err), tmpRoot, logDir)
	}

	hostSocket := filepath.Join(runDir, "levikd.sock")
	orchestratorSocket := filepath.Join(runDir, "levik-orchestrator.sock")
	consoleAddr := fmt.Sprintf("127.0.0.1:%d", consolePort)
	dashboardAddr := fmt.Sprintf("127.0.0.1:%d", dashboardPort)
	consoleAPIKey := "levik-smoke-key"

	gatewayProc, err := startSmokeProcess(ctx, smokeProcessConfig{
		projectRoot:        projectRoot,
		tempHome:           homeDir,
		logDir:             logDir,
		logName:            "gateway.log",
		args:               []string{"gateway"},
		hostSocket:         hostSocket,
		orchestratorSocket: orchestratorSocket,
		consoleAddr:        consoleAddr,
		dashboardAddr:      dashboardAddr,
		consoleAPIKey:      consoleAPIKey,
	})
	if err != nil {
		return attachSmokeContext(err, tmpRoot, logDir)
	}
	defer stopSmokeProcess(gatewayProc)

	consoleClient := &http.Client{Timeout: 5 * time.Second}
	if err := waitForConsole(ctx, consoleClient, consoleAddr, consoleAPIKey, gatewayProc); err != nil {
		return attachSmokeContext(err, tmpRoot, logDir)
	}

	if err := smokeConsoleJSON(ctx, consoleClient, consoleAddr, consoleAPIKey, http.MethodPost, "/api/orchestrator/start", nil, nil); err != nil {
		return attachSmokeContext(fmt.Errorf("start orchestrator via console: %w", err), tmpRoot, logDir)
	}

	if err := waitForOrchestrator(ctx, consoleClient, consoleAddr, consoleAPIKey, gatewayProc); err != nil {
		return attachSmokeContext(err, tmpRoot, logDir)
	}

	createPayload := map[string]interface{}{
		"objective": "Prepare a bounded documentation update and merge-ready handoff for the smoke repository.",
		"repo_path": repoDir,
	}
	var submitResp smokeConsoleTaskSubmitResponse
	if err := smokeConsoleJSON(ctx, consoleClient, consoleAddr, consoleAPIKey, http.MethodPost, "/api/tasks", createPayload, &submitResp); err != nil {
		return attachSmokeContext(fmt.Errorf("submit console task: %w", err), tmpRoot, logDir)
	}
	if submitResp.TaskID == "" {
		return attachSmokeContext(fmt.Errorf("console task submission returned empty task_id"), tmpRoot, logDir)
	}
	if submitResp.Task.Phase != "change_ready" {
		return attachSmokeContext(fmt.Errorf("expected initial phase change_ready, got %q", submitResp.Task.Phase), tmpRoot, logDir)
	}

	var taskList smokeConsoleTaskList
	if err := smokeConsoleJSON(ctx, consoleClient, consoleAddr, consoleAPIKey, http.MethodGet, "/api/tasks", nil, &taskList); err != nil {
		return attachSmokeContext(fmt.Errorf("list console tasks: %w", err), tmpRoot, logDir)
	}
	if !containsSmokeTask(taskList.Tasks, submitResp.TaskID) {
		return attachSmokeContext(fmt.Errorf("console task list did not include %s", submitResp.TaskID), tmpRoot, logDir)
	}

	orchestratorClient := newSmokeUnixClient(orchestratorSocket, 30*time.Second)
	changeRequest := smokeTaskChangeRequest{
		TaskID:  submitResp.TaskID,
		Summary: "Add one founder-facing line to README and verify merge readiness.",
		Edits: []smokeTextReplacement{
			{
				Path:      "README.md",
				OldText:   "Initial note.\n",
				NewText:   "Initial note.\nSmoked by LeVik.\n",
				Rationale: "Bounded documentation edit for smoke verification.",
			},
		},
	}
	var changedTask smokeTaskSession
	if err := smokeUnixJSON(ctx, orchestratorClient, "http://levik-orchestrator/v1/tasks/"+submitResp.TaskID+"/changes", changeRequest, &changedTask); err != nil {
		return attachSmokeContext(fmt.Errorf("apply bounded change: %w", err), tmpRoot, logDir)
	}
	if changedTask.Phase != "merge_ready" || changedTask.Status != "completed" {
		return attachSmokeContext(fmt.Errorf("expected merge_ready/completed, got %s/%s", changedTask.Phase, changedTask.Status), tmpRoot, logDir)
	}
	if changedTask.MergeReadiness != "ready" {
		return attachSmokeContext(fmt.Errorf("expected merge readiness ready, got %q", changedTask.MergeReadiness), tmpRoot, logDir)
	}

	var finalTask smokeTaskSession
	if err := smokeUnixJSON(ctx, orchestratorClient, "http://levik-orchestrator/v1/tasks/"+submitResp.TaskID, nil, &finalTask); err != nil {
		return attachSmokeContext(fmt.Errorf("fetch final task: %w", err), tmpRoot, logDir)
	}
	if finalTask.Phase != "merge_ready" {
		return attachSmokeContext(fmt.Errorf("final task phase = %q, want merge_ready", finalTask.Phase), tmpRoot, logDir)
	}

	var review smokeTaskReview
	if err := smokeUnixJSON(ctx, orchestratorClient, "http://levik-orchestrator/v1/tasks/"+submitResp.TaskID+"/review", nil, &review); err != nil {
		return attachSmokeContext(fmt.Errorf("fetch review detail: %w", err), tmpRoot, logDir)
	}
	if review.ChangeArtifactPath == "" || review.MergeArtifactPath == "" {
		return attachSmokeContext(fmt.Errorf("expected change and merge artifacts, got change=%q merge=%q", review.ChangeArtifactPath, review.MergeArtifactPath), tmpRoot, logDir)
	}
	if !hasSmokeArtifactPreview(review.ArtifactPreviews, "Archive Email Draft", "archive") {
		return attachSmokeContext(fmt.Errorf("expected archive email draft artifact preview"), tmpRoot, logDir)
	}

	changeArtifactURL := "http://levik-orchestrator/v1/tasks/" + submitResp.TaskID + "/artifacts/content?path=" + url.QueryEscape(review.ChangeArtifactPath)
	var changeArtifact smokeArtifactContent
	if err := smokeUnixJSON(ctx, orchestratorClient, changeArtifactURL, nil, &changeArtifact); err != nil {
		return attachSmokeContext(fmt.Errorf("read change artifact: %w", err), tmpRoot, logDir)
	}
	if !strings.Contains(changeArtifact.Content, "Applied Change") {
		return attachSmokeContext(fmt.Errorf("change artifact content missing heading"), tmpRoot, logDir)
	}

	mergeArtifactURL := "http://levik-orchestrator/v1/tasks/" + submitResp.TaskID + "/artifacts/content?path=" + url.QueryEscape(review.MergeArtifactPath)
	var mergeArtifact smokeArtifactContent
	if err := smokeUnixJSON(ctx, orchestratorClient, mergeArtifactURL, nil, &mergeArtifact); err != nil {
		return attachSmokeContext(fmt.Errorf("read merge artifact: %w", err), tmpRoot, logDir)
	}
	if !strings.Contains(mergeArtifact.Content, "Merge Readiness") {
		return attachSmokeContext(fmt.Errorf("merge artifact content missing heading"), tmpRoot, logDir)
	}

	var finalConsoleList smokeConsoleTaskList
	if err := smokeConsoleJSON(ctx, consoleClient, consoleAddr, consoleAPIKey, http.MethodGet, "/api/tasks", nil, &finalConsoleList); err != nil {
		return attachSmokeContext(fmt.Errorf("list tasks after change: %w", err), tmpRoot, logDir)
	}
	finalConsoleTask, ok := findSmokeTask(finalConsoleList.Tasks, submitResp.TaskID)
	if !ok {
		return attachSmokeContext(fmt.Errorf("console task list lost task %s", submitResp.TaskID), tmpRoot, logDir)
	}
	if finalConsoleTask.Status != "completed" || finalConsoleTask.Phase != "merge_ready" {
		return attachSmokeContext(fmt.Errorf("console task shows %s/%s, want completed/merge_ready", finalConsoleTask.Status, finalConsoleTask.Phase), tmpRoot, logDir)
	}

	founderTaskPayload := map[string]interface{}{
		"objective": "Exercise founder approval for a bounded code change in the smoke repository.",
		"repo_path": repoDir,
	}
	var founderSubmit smokeConsoleTaskSubmitResponse
	if err := smokeConsoleJSON(ctx, consoleClient, consoleAddr, consoleAPIKey, http.MethodPost, "/api/tasks", founderTaskPayload, &founderSubmit); err != nil {
		return attachSmokeContext(fmt.Errorf("submit founder approval task: %w", err), tmpRoot, logDir)
	}
	if founderSubmit.TaskID == "" {
		return attachSmokeContext(fmt.Errorf("founder approval task returned empty task_id"), tmpRoot, logDir)
	}

	founderChange := smokeTaskChangeRequest{
		TaskID:  founderSubmit.TaskID,
		Summary: "Change a tracked Go source file and require founder approval.",
		Edits: []smokeTextReplacement{
			{
				Path:      "main.go",
				OldText:   "fmt.Println(\"smoke\")",
				NewText:   "fmt.Println(\"smoke approved\")",
				Rationale: "Bounded code edit to exercise the founder approval path.",
			},
		},
	}
	var approvalTask smokeTaskSession
	if err := smokeUnixJSON(ctx, orchestratorClient, "http://levik-orchestrator/v1/tasks/"+founderSubmit.TaskID+"/changes", founderChange, &approvalTask); err != nil {
		return attachSmokeContext(fmt.Errorf("apply founder approval change: %w", err), tmpRoot, logDir)
	}
	if approvalTask.Phase != "founder_review_requested" || approvalTask.Status != "awaiting_approval" {
		return attachSmokeContext(fmt.Errorf("expected founder_review_requested/awaiting_approval, got %s/%s", approvalTask.Phase, approvalTask.Status), tmpRoot, logDir)
	}
	if approvalTask.RiskClass != "high" || approvalTask.ApprovalRoute != "founder_review" {
		return attachSmokeContext(fmt.Errorf("expected high/founder_review, got %s/%s", approvalTask.RiskClass, approvalTask.ApprovalRoute), tmpRoot, logDir)
	}

	resumeRequest := smokeTaskResumeRequest{
		TaskID:        founderSubmit.TaskID,
		Decision:      "approve",
		Comment:       "Smoke approval for bounded code change",
		ProposedEdits: map[string]interface{}{},
	}
	var resumedTask smokeTaskSession
	if err := smokeUnixJSON(ctx, orchestratorClient, "http://levik-orchestrator/v1/tasks/"+founderSubmit.TaskID+"/resume", resumeRequest, &resumedTask); err != nil {
		return attachSmokeContext(fmt.Errorf("resume founder approval task: %w", err), tmpRoot, logDir)
	}
	if resumedTask.Phase != "merge_ready" || resumedTask.Status != "completed" {
		return attachSmokeContext(fmt.Errorf("expected approved task merge_ready/completed, got %s/%s", resumedTask.Phase, resumedTask.Status), tmpRoot, logDir)
	}
	if resumedTask.MergeReadiness != "ready" {
		return attachSmokeContext(fmt.Errorf("expected approved task merge readiness ready, got %q", resumedTask.MergeReadiness), tmpRoot, logDir)
	}

	fmt.Printf("✓ Orchestrator smoke passed\n")
	fmt.Printf("  temp root: %s\n", tmpRoot)
	fmt.Printf("  console:   http://%s\n", consoleAddr)
	fmt.Printf("  tasks:     %s, %s\n", submitResp.TaskID, founderSubmit.TaskID)
	if !opts.keepTemp {
		fmt.Println("  cleanup:   automatic")
	}
	return nil
}

func buildSmokeConfig(workspaceDir, stubURL string, gatewayPort int) *config.Config {
	cfg := config.DefaultConfig()
	cfg.Workspace.Path = workspaceDir
	cfg.Workspace.Sandboxed = false
	cfg.Agents.Defaults.Workspace = workspaceDir
	cfg.Agents.Defaults.RestrictToWorkspace = true
	cfg.Agents.Defaults.Provider = "vllm"
	cfg.Agents.Defaults.Model = "fake-model"
	cfg.Agents.Defaults.MaxToolIterations = 8
	cfg.Providers.VLLM.APIBase = stubURL
	cfg.Gateway.Host = "127.0.0.1"
	cfg.Gateway.Port = gatewayPort
	cfg.Heartbeat.Enabled = false
	cfg.MCP.Enabled = false
	cfg.Channels.Telegram.Enabled = false
	cfg.Channels.WhatsApp.Enabled = false
	cfg.V1API.Enabled = false
	cfg.Agents.List = []config.AgentConfig{
		{ID: "lead", Name: "Lead", Role: "lead", Provider: "vllm", Model: "fake-model", Workspace: filepath.Join(workspaceDir, "agents", "lead")},
		{ID: "engineer", Name: "Engineer", Role: "engineer", Provider: "vllm", Model: "fake-model", Workspace: filepath.Join(workspaceDir, "agents", "engineer")},
		{ID: "runner", Name: "Runner", Role: "runner", Provider: "vllm", Model: "fake-model", Workspace: filepath.Join(workspaceDir, "agents", "runner")},
		{ID: "reviewer", Name: "Reviewer", Role: "reviewer", Provider: "vllm", Model: "fake-model", Workspace: filepath.Join(workspaceDir, "agents", "reviewer")},
	}
	return cfg
}

func smokeRepoPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, "repos", "smoke-repo")
}

func detectProjectRoot() (string, error) {
	candidates := []string{
		strings.TrimSpace(os.Getenv("LEVIK_PROJECT_ROOT")),
		".",
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		root := candidate
		for depth := 0; depth < 6; depth++ {
			if fileExists(filepath.Join(root, "go.mod")) && fileExists(filepath.Join(root, "services", "orchestrator", "pyproject.toml")) {
				absRoot, err := filepath.Abs(root)
				if err != nil {
					return "", err
				}
				return absRoot, nil
			}
			next := filepath.Dir(root)
			if next == root {
				break
			}
			root = next
		}
	}
	return "", fmt.Errorf("could not locate LeVik project root")
}

func startSmokeLLMStub() (string, func(context.Context) error, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		prompt := ""
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if strings.TrimSpace(req.Messages[i].Content) != "" {
				prompt = req.Messages[i].Content
				break
			}
		}

		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":       map[string]interface{}{"content": smokeLLMReply(prompt)},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     32,
				"completion_tokens": 24,
				"total_tokens":      56,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	server := &http.Server{Handler: handler}
	go func() {
		_ = server.Serve(listener)
	}()

	return "http://" + listener.Addr().String() + "/v1", server.Shutdown, nil
}

func smokeLLMReply(prompt string) string {
	switch {
	case strings.Contains(prompt, "You are a Devil's Advocate"):
		return "CONCEDE"
	case strings.Contains(prompt, `"verdict": "APPROVE" | "CHANGES_REQUESTED" | "REJECT"`):
		return `{"verdict":"APPROVE","issues":[],"summary":"Bounded change matches the objective and stays within scope."}`
	case strings.Contains(prompt, `"verdict": "PASSED" or "FAILED"`):
		return `{"verdict":"PASSED","summary":"Focused verification passed cleanly.","issues":[]}`
	case strings.Contains(prompt, "Produce exact code changes"):
		return "[]"
	case strings.Contains(prompt, "Write a Node.js script using Playwright"):
		return "console.log('qa skipped for smoke');"
	case strings.Contains(prompt, "create a concrete implementation plan"):
		return "1. Update README.md.\n2. Keep the change bounded to documentation.\n3. Verify the repository stays merge-ready."
	default:
		return "Smoke stub response"
	}
}

func createSmokeRepo(ctx context.Context, repoDir string) error {
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Smoke Repo\n\nInitial note.\n"), 0o644); err != nil {
		return err
	}
	mainSource := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"smoke\")\n}\n"
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte(mainSource), 0o644); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "init"); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "config", "user.name", "LeVik Smoke"); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "config", "user.email", "smoke@levik.local"); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "add", "README.md", "main.go"); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "commit", "-m", "initial"); err != nil {
		return err
	}
	if err := runSmokeGit(ctx, repoDir, "branch", "-M", "main"); err != nil {
		return err
	}
	return nil
}

func runSmokeGit(ctx context.Context, repoDir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repoDir}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func pickFreeTCPPort() (int, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address %T", listener.Addr())
	}
	return addr.Port, nil
}

type smokeProcessConfig struct {
	projectRoot        string
	tempHome           string
	logDir             string
	logName            string
	args               []string
	hostSocket         string
	orchestratorSocket string
	consoleAddr        string
	dashboardAddr      string
	consoleAPIKey      string
}

func startSmokeProcess(ctx context.Context, cfg smokeProcessConfig) (*smokeProcess, error) {
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	logPath := filepath.Join(cfg.logDir, cfg.logName)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, executable, cfg.args...)
	cmd.Dir = cfg.projectRoot
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Env = append(os.Environ(),
		config.HomeEnvVar+"="+cfg.tempHome,
		"LEVIK_PROJECT_ROOT="+cfg.projectRoot,
		"LEVIK_HOST_SOCKET="+cfg.hostSocket,
		"LEVIK_ORCHESTRATOR_SOCKET="+cfg.orchestratorSocket,
		"LEVIK_CONSOLE_ENABLED=1",
		"LEVIK_CONSOLE_ADDR="+cfg.consoleAddr,
		"LEVIK_DASHBOARD_ADDR="+cfg.dashboardAddr,
		"LEVIK_CONSOLE_API_KEY="+cfg.consoleAPIKey,
	)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start %s: %w", strings.Join(cfg.args, " "), err)
	}
	done := make(chan error, 1)
	go func() {
		defer logFile.Close()
		done <- cmd.Wait()
	}()
	return &smokeProcess{cmd: cmd, logPath: logPath, done: done}, nil
}

func stopSmokeProcess(proc *smokeProcess) {
	if proc == nil || proc.cmd == nil || proc.cmd.Process == nil {
		return
	}
	if proc.cmd.ProcessState != nil {
		return
	}
	_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-proc.done:
	case <-time.After(5 * time.Second):
		_ = proc.cmd.Process.Kill()
		<-proc.done
	}
}

func waitForConsole(ctx context.Context, client *http.Client, consoleAddr, apiKey string, proc *smokeProcess) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-proc.done:
			return fmt.Errorf("gateway exited before console became ready: %w", err)
		default:
		}

		var health smokeConsoleHealth
		err := smokeConsoleJSON(ctx, client, consoleAddr, apiKey, http.MethodGet, "/api/health", nil, &health)
		if err == nil && health.Status == "ok" {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for console at %s", consoleAddr)
}

func waitForOrchestrator(ctx context.Context, client *http.Client, consoleAddr, apiKey string, proc *smokeProcess) error {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-proc.done:
			return fmt.Errorf("gateway exited before orchestrator became ready: %w", err)
		default:
		}

		var status smokeConsoleOrchestratorStatus
		err := smokeConsoleJSON(ctx, client, consoleAddr, apiKey, http.MethodGet, "/api/orchestrator", nil, &status)
		if err == nil && status.Reachable {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for orchestrator via console")
}

func smokeConsoleJSON(ctx context.Context, client *http.Client, consoleAddr, apiKey, method, path string, body interface{}, out interface{}) error {
	url := "http://" + consoleAddr + path
	return smokeHTTPJSON(ctx, client, url, apiKey, method, body, out)
}

func smokeHTTPJSON(ctx context.Context, client *http.Client, targetURL, apiKey, method string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func newSmokeUnixClient(socketPath string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

func smokeUnixJSON(ctx context.Context, client *http.Client, targetURL string, body interface{}, out interface{}) error {
	return smokeHTTPJSON(ctx, client, targetURL, "", httpMethodForBody(body), body, out)
}

func httpMethodForBody(body interface{}) string {
	if body == nil {
		return http.MethodGet
	}
	return http.MethodPost
}

func containsSmokeTask(tasks []smokeConsoleTask, taskID string) bool {
	_, ok := findSmokeTask(tasks, taskID)
	return ok
}

func findSmokeTask(tasks []smokeConsoleTask, taskID string) (smokeConsoleTask, bool) {
	for _, task := range tasks {
		if task.TaskID == taskID {
			return task, true
		}
	}
	return smokeConsoleTask{}, false
}

func hasSmokeArtifactPreview(previews []struct {
	Title string `json:"title"`
	Kind  string `json:"kind"`
	Path  string `json:"path"`
}, title, kind string) bool {
	for _, preview := range previews {
		if preview.Title == title && preview.Kind == kind && strings.TrimSpace(preview.Path) != "" {
			return true
		}
	}
	return false
}

func attachSmokeContext(err error, tmpRoot, logDir string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w (temp root: %s, logs: %s)", err, tmpRoot, logDir)
}

func consoleAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("LEVIK_CONSOLE_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:18793"
}

func dashboardAddrFromEnv() string {
	if addr := strings.TrimSpace(os.Getenv("LEVIK_DASHBOARD_ADDR")); addr != "" {
		return addr
	}
	return "127.0.0.1:18792"
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

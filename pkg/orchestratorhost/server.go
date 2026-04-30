package orchestratorhost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/v1claw/levik/pkg/logger"
	"github.com/v1claw/levik/pkg/orchestrator"
	"github.com/v1claw/levik/pkg/tools"
)

const maxInboundBodyBytes = 1 << 20
const maxRepoPreviewBytes = 1200
const defaultReadMaxBytes = 4000
const defaultArtifactReadMaxBytes = 32000
const maxArtifactReadBytes = 128 * 1024
const maxWriteBytes = 64 * 1024
const defaultTargetLimit = 6
const maxTargetDiscoveryFiles = 800
const maxTargetFileBytes = 256 * 1024
const defaultVerificationLimit = 6

var taskIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var artifactIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var objectiveTokenPattern = regexp.MustCompile(`[A-Za-z0-9_]+`)

type notifier interface {
	SendToChannel(ctx context.Context, channelName, chatID, content string) error
}

type execTool interface {
	Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult
}

// ReviewFunc is called by the /v1/review/change endpoint to obtain an
// independent LLM review verdict.  The caller provides task context; the
// implementation is responsible for selecting the reviewer model.
type ReviewFunc func(ctx context.Context, req orchestrator.ChangeReviewRequest) (orchestrator.ChangeReviewResponse, error)

// ThinkFunc is called by the /v1/agent/think endpoint to run a reasoning
// step requested by the Python orchestrator. The host executes the requested
// provider/model route and remains the native capability boundary.
type ThinkFunc func(ctx context.Context, req orchestrator.AgentThinkRequest) (orchestrator.AgentThinkResponse, error)

// Config defines the native host capability surface exposed to the orchestrator.
type Config struct {
	SocketPath          string
	WorkspaceRoot       string
	RestrictToWorkspace bool
	Sandboxed           bool
	TelegramEnabled     bool
	ReviewChange        ReviewFunc // optional LLM judge integration
	AgentThink          ThinkFunc  // optional per-role LLM reasoning
	AgentRoster         []orchestrator.AgentProfile
}

// Server exposes the host daemon capability surface over a Unix domain socket.
type Server struct {
	cfg        Config
	notifier   notifier
	execTool   execTool
	httpServer *http.Server
	listener   net.Listener
	mu         sync.Mutex
}

// NewServer builds a host capability server around the current LeVik runtime.
func NewServer(cfg Config, notifier notifier) *Server {
	return &Server{
		cfg:      cfg,
		notifier: notifier,
		execTool: tools.NewExecToolForWorkspace(cfg.WorkspaceRoot, cfg.RestrictToWorkspace, cfg.Sandboxed, nil),
	}
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/v1/system/health", s.handleHealthz)
	mux.HandleFunc("/v1/workspaces/provision", s.handleProvisionWorkspace)
	mux.HandleFunc("/v1/git/worktrees/create", s.handleCreateWorktree)
	mux.HandleFunc("/v1/git/worktrees/remove", s.handleRemoveWorktree)
	mux.HandleFunc("/v1/repos/inspect", s.handleInspectRepo)
	mux.HandleFunc("/v1/repos/discover-targets", s.handleDiscoverTargets)
	mux.HandleFunc("/v1/repos/discover-verification", s.handleDiscoverVerification)
	mux.HandleFunc("/v1/files/read", s.handleReadFile)
	mux.HandleFunc("/v1/files/write", s.handleWriteFile)
	mux.HandleFunc("/v1/files/replace", s.handleReplaceFile)
	mux.HandleFunc("/v1/artifacts/write", s.handleWriteArtifact)
	mux.HandleFunc("/v1/artifacts/read", s.handleReadArtifact)
	mux.HandleFunc("/v1/exec", s.handleExec)
	mux.HandleFunc("/v1/notify/telegram", s.handleNotifyTelegram)
	mux.HandleFunc("/v1/git/rollback", s.handleRollbackWorktree)
	mux.HandleFunc("/v1/review/change", s.handleReviewChange)
	mux.HandleFunc("/v1/agent/roster", s.handleAgentRoster)
	mux.HandleFunc("/v1/agent/think", s.handleAgentThink)
	mux.HandleFunc("/v1/repos/discover-lint", s.handleDiscoverLint)
	mux.HandleFunc("/v1/repos/run-lint", s.handleRunLint)
	mux.HandleFunc("/v1/browser/test", s.handleBrowserTest)
	return mux
}

// Start serves the host capability API on a Unix domain socket.
func (s *Server) Start(ctx context.Context) error {
	if strings.TrimSpace(s.cfg.SocketPath) == "" {
		return fmt.Errorf("socket path is required")
	}
	if strings.TrimSpace(s.cfg.WorkspaceRoot) == "" {
		return fmt.Errorf("workspace root is required")
	}

	if err := os.MkdirAll(filepath.Dir(s.cfg.SocketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := removeStaleSocket(s.cfg.SocketPath); err != nil {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", s.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket: %w", err)
	}
	if err := os.Chmod(s.cfg.SocketPath, 0o600); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	s.mu.Lock()
	s.listener = listener
	s.httpServer = &http.Server{
		Handler:      s.handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	s.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = s.Stop(context.Background())
	}()

	logger.InfoCF("orchestrator-host", "Host capability server starting", map[string]interface{}{
		"socket_path":           s.cfg.SocketPath,
		"workspace_root":        s.cfg.WorkspaceRoot,
		"restrict_to_workspace": s.cfg.RestrictToWorkspace,
		"sandboxed":             s.cfg.Sandboxed,
	})

	err = s.httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// Stop gracefully shuts down the host capability server.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var shutdownErr error
	if s.httpServer != nil {
		shutdownErr = s.httpServer.Shutdown(ctx)
		s.httpServer = nil
	}
	if s.listener != nil {
		_ = s.listener.Close()
		s.listener = nil
	}
	if s.cfg.SocketPath != "" {
		_ = os.Remove(s.cfg.SocketPath)
	}
	return shutdownErr
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.SystemHealthResponse{
		Status:              "ok",
		WorkspaceRoot:       s.cfg.WorkspaceRoot,
		SocketPath:          s.cfg.SocketPath,
		RestrictToWorkspace: s.cfg.RestrictToWorkspace,
		Sandboxed:           s.cfg.Sandboxed,
		TelegramEnabled:     s.cfg.TelegramEnabled,
	})
}

func (s *Server) handleProvisionWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.WorkspaceProvisionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}

	taskRoot := filepath.Join(s.cfg.WorkspaceRoot, "tasks", req.TaskID)
	artifactsDir := filepath.Join(taskRoot, "artifacts")
	logsDir := filepath.Join(taskRoot, "logs")
	scratchDir := filepath.Join(taskRoot, "scratch")
	worktreePath := filepath.Join(s.cfg.WorkspaceRoot, "worktrees", req.TaskID)

	for _, path := range []string{artifactsDir, logsDir, scratchDir, filepath.Dir(worktreePath)} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to provision workspace: %v", err)})
			return
		}
	}

	writeJSON(w, http.StatusOK, orchestrator.WorkspaceProvisionResponse{
		TaskID:       req.TaskID,
		TaskRoot:     taskRoot,
		ArtifactsDir: artifactsDir,
		LogsDir:      logsDir,
		ScratchDir:   scratchDir,
		WorktreePath: worktreePath,
	})
}

func (s *Server) handleCreateWorktree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.GitWorktreeCreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Repo.Path) == "" || strings.TrimSpace(req.WorktreePath) == "" || strings.TrimSpace(req.Branch) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "repo.path, worktree_path, and branch are required"})
		return
	}

	worktreePath, err := filepath.Abs(req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid worktree_path"})
		return
	}
	worktreeRoot := filepath.Join(s.cfg.WorkspaceRoot, "worktrees")
	if !isWithinRoot(worktreePath, worktreeRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path must remain inside the managed worktree root"})
		return
	}

	baseRef := strings.TrimSpace(req.BaseRef)
	if baseRef == "" {
		baseRef = req.Repo.DefaultBranch
	}
	if strings.TrimSpace(baseRef) == "" {
		baseRef = "main"
	}

	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to prepare worktree parent: %v", err)})
		return
	}

	if _, err := os.Stat(filepath.Join(worktreePath, ".git")); err == nil {
		headRef, headErr := gitHeadRef(r.Context(), worktreePath)
		if headErr != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect existing worktree: %v", headErr)})
			return
		}
		writeJSON(w, http.StatusOK, orchestrator.GitWorktreeCreateResponse{
			TaskID:       req.TaskID,
			RepoPath:     req.Repo.Path,
			WorktreePath: worktreePath,
			Branch:       req.Branch,
			BaseRef:      baseRef,
			HeadRef:      headRef,
			Created:      false,
		})
		return
	}

	output, err := runGit(r.Context(), req.Repo.Path, "worktree", "add", "-b", req.Branch, worktreePath, baseRef)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to create worktree: %v: %s", err, strings.TrimSpace(output))})
		return
	}

	headRef, headErr := gitHeadRef(r.Context(), worktreePath)
	if headErr != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect worktree head: %v", headErr)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.GitWorktreeCreateResponse{
		TaskID:       req.TaskID,
		RepoPath:     req.Repo.Path,
		WorktreePath: worktreePath,
		Branch:       req.Branch,
		BaseRef:      baseRef,
		HeadRef:      headRef,
		Created:      true,
	})
}

func (s *Server) handleRemoveWorktree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.GitWorktreeRemoveRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.WorktreePath) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path is required"})
		return
	}

	worktreePath, err := filepath.Abs(req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid worktree_path"})
		return
	}
	worktreeRoot := filepath.Join(s.cfg.WorkspaceRoot, "worktrees")
	if !isWithinRoot(worktreePath, worktreeRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path must remain inside the managed worktree root"})
		return
	}
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		writeJSON(w, http.StatusOK, orchestrator.GitWorktreeRemoveResponse{
			TaskID:       req.TaskID,
			WorktreePath: worktreePath,
			Removed:      false,
		})
		return
	}

	args := []string{"worktree", "remove"}
	if req.Force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	output, err := runGit(r.Context(), worktreePath, args...)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to remove worktree: %v: %s", err, strings.TrimSpace(output))})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.GitWorktreeRemoveResponse{
		TaskID:       req.TaskID,
		WorktreePath: worktreePath,
		Removed:      true,
	})
}

func (s *Server) handleInspectRepo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.RepoInspectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.WorktreePath) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path is required"})
		return
	}

	worktreePath, err := filepath.Abs(req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid worktree_path"})
		return
	}
	worktreeRoot := filepath.Join(s.cfg.WorkspaceRoot, "worktrees")
	if !isWithinRoot(worktreePath, worktreeRoot) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path must remain inside the managed worktree root"})
		return
	}

	branch, err := gitBranchName(r.Context(), worktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect branch: %v", err)})
		return
	}
	headRef, err := gitCommitRef(r.Context(), worktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect head: %v", err)})
		return
	}
	statusLines, err := gitStatusLines(r.Context(), worktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect git status: %v", err)})
		return
	}
	changedFiles, additions, deletions, diffShortStat, err := gitChangedFiles(r.Context(), worktreePath, statusLines)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect git diff summary: %v", err)})
		return
	}
	entries, err := topLevelEntries(worktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect repository entries: %v", err)})
		return
	}
	keyFiles, err := inspectKeyFiles(worktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to inspect repository files: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.RepoInspectResponse{
		TaskID:           req.TaskID,
		RepoPath:         req.RepoPath,
		WorktreePath:     worktreePath,
		Branch:           branch,
		HeadRef:          headRef,
		Dirty:            len(statusLines) > 0,
		ChangedFileCount: len(changedFiles),
		Additions:        additions,
		Deletions:        deletions,
		DiffShortStat:    diffShortStat,
		TopLevelEntries:  entries,
		StatusLines:      statusLines,
		ChangedFiles:     changedFiles,
		KeyFiles:         keyFiles,
	})
}

func (s *Server) handleDiscoverTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.RepoTargetDiscoveryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	limit := req.Limit
	if limit <= 0 {
		limit = defaultTargetLimit
	}
	candidates, err := discoverRepoTargets(worktreePath, req.Objective, limit)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to discover targets: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.RepoTargetDiscoveryResponse{
		TaskID:       req.TaskID,
		WorktreePath: worktreePath,
		Candidates:   candidates,
	})
}

func (s *Server) handleReadFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.FileReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fullPath, err := resolveWorktreeFilePath(worktreePath, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultReadMaxBytes
	}
	content, bytesRead, truncated, err := readBoundedFile(fullPath, maxBytes)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to read file: %v", err)})
		return
	}

	relativePath, relErr := filepath.Rel(worktreePath, fullPath)
	if relErr != nil {
		relativePath = req.Path
	}
	writeJSON(w, http.StatusOK, orchestrator.FileReadResponse{
		TaskID:    req.TaskID,
		Path:      relativePath,
		FullPath:  fullPath,
		Content:   content,
		BytesRead: bytesRead,
		Truncated: truncated,
	})
}

func (s *Server) handleWriteFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.FileWriteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if len(req.Content) > maxWriteBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("content exceeds %d bytes", maxWriteBytes)})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fullPath, err := resolveWorktreeFilePath(worktreePath, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Verify rollback base before the first edit.
	if err := ensureSnapshot(r.Context(), worktreePath, req.TaskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to verify rollback base: %v", err)})
		return
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to create parent directory: %v", err)})
		return
	}
	if err := os.WriteFile(fullPath, []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to write file: %v", err)})
		return
	}

	relativePath, relErr := filepath.Rel(worktreePath, fullPath)
	if relErr != nil {
		relativePath = req.Path
	}
	writeJSON(w, http.StatusOK, orchestrator.FileWriteResponse{
		TaskID:       req.TaskID,
		Path:         relativePath,
		FullPath:     fullPath,
		BytesWritten: len(req.Content),
	})
}

func (s *Server) handleReplaceFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.FileReplaceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}
	if req.OldText == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "old_text is required"})
		return
	}
	if len(req.NewText) > maxWriteBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("new_text exceeds %d bytes", maxWriteBytes)})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fullPath, err := resolveWorktreeFilePath(worktreePath, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// Verify rollback base before the first edit.
	if err := ensureSnapshot(r.Context(), worktreePath, req.TaskID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to verify rollback base: %v", err)})
		return
	}
	bytesWritten, err := replaceUniqueText(fullPath, req.OldText, req.NewText)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	relativePath, relErr := filepath.Rel(worktreePath, fullPath)
	if relErr != nil {
		relativePath = req.Path
	}
	writeJSON(w, http.StatusOK, orchestrator.FileReplaceResponse{
		TaskID:       req.TaskID,
		Path:         relativePath,
		FullPath:     fullPath,
		BytesWritten: bytesWritten,
	})
}

func (s *Server) handleDiscoverVerification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.VerificationDiscoveryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	runtime, candidates, err := discoverVerificationCommands(worktreePath, req.TargetPaths)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to discover verification commands: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.VerificationDiscoveryResponse{
		TaskID:       req.TaskID,
		WorktreePath: worktreePath,
		Runtime:      runtime,
		Candidates:   candidates,
	})
}

func (s *Server) handleWriteArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.ArtifactWriteRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.Artifact.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact.task_id contains unsupported characters"})
		return
	}
	if !artifactIDPattern.MatchString(req.Artifact.ArtifactID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "artifact.artifact_id contains unsupported characters"})
		return
	}

	format := normalizedArtifactFormat(req.Format)
	artifactsDir := filepath.Join(s.cfg.WorkspaceRoot, "tasks", req.Artifact.TaskID, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to prepare artifacts directory: %v", err)})
		return
	}

	artifactPath := filepath.Join(artifactsDir, req.Artifact.ArtifactID+"."+format)
	if err := os.WriteFile(artifactPath, []byte(req.Content), 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("failed to write artifact: %v", err)})
		return
	}

	artifact := req.Artifact
	artifact.Path = artifactPath
	writeJSON(w, http.StatusOK, orchestrator.ArtifactWriteResponse{
		Artifact:     artifact,
		Path:         artifactPath,
		BytesWritten: len(req.Content),
	})
}

func (s *Server) handleReadArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.ArtifactReadRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Path) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path is required"})
		return
	}

	artifactsRoot := filepath.Join(s.cfg.WorkspaceRoot, "tasks", req.TaskID, "artifacts")
	artifactPath, err := resolveArtifactPath(artifactsRoot, req.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultArtifactReadMaxBytes
	}
	if maxBytes > maxArtifactReadBytes {
		maxBytes = maxArtifactReadBytes
	}

	content, bytesRead, truncated, err := readBoundedFile(artifactPath, maxBytes)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to read artifact: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.ArtifactReadResponse{
		TaskID:    req.TaskID,
		Path:      artifactPath,
		Content:   content,
		BytesRead: bytesRead,
		Truncated: truncated,
	})
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.HostActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.ActionName != "exec" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action_name must be exec"})
		return
	}

	commandValue, ok := req.Arguments["command"]
	command, commandOK := commandValue.(string)
	if !ok || !commandOK || strings.TrimSpace(command) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "arguments.command is required"})
		return
	}

	args := map[string]interface{}{
		"command": command,
	}
	if req.WorkingDir != "" {
		args["working_dir"] = req.WorkingDir
	}

	result := s.execTool.Execute(r.Context(), tools.ToolContext{}, args)
	state := map[string]interface{}{}
	if req.WorkingDir != "" {
		state["working_dir"] = req.WorkingDir
	}

	// Shape the observation with SWE-agent-style templates that guide
	// the model toward better behaviour on empty/truncated output.
	shaped := shapeObservation(result.ForLLM, result.IsError)
	if req.WorkingDir != "" {
		shaped = shaped + fmt.Sprintf("\n[working_dir: %s]", req.WorkingDir)
	}

	obs := orchestrator.HostObservation{
		TaskID:     req.TaskID,
		ActionName: req.ActionName,
		Success:    !result.IsError,
		Summary:    summarizeObservation(result.ForLLM, result.IsError),
		Output:     shaped,
		State:      state,
	}
	// Populate exit code when available from the exec tool.
	if result.ExitCode != nil {
		exitCode := int(*result.ExitCode)
		obs.ExitCode = &exitCode
	}
	writeJSON(w, http.StatusOK, obs)
}

func (s *Server) handleNotifyTelegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.notifier == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "telegram notifier is not available"})
		return
	}

	var req orchestrator.ChannelNotificationRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.ChatID) == "" || strings.TrimSpace(req.Content) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "chat_id and content are required"})
		return
	}

	channel := req.Channel
	if strings.TrimSpace(channel) == "" {
		channel = "telegram"
	}
	if channel != "telegram" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "only telegram notifications are supported by this endpoint"})
		return
	}

	if err := s.notifier.SendToChannel(r.Context(), channel, req.ChatID, req.Content); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to deliver telegram message: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, orchestrator.ChannelNotificationResponse{
		Delivered: true,
		Summary:   "telegram notification delivered",
	})
}

// handleRollbackWorktree reverts a managed task worktree to its current HEAD.
// Task worktrees are created per task, so rollback must remove both tracked
// edits and new untracked files instead of relying on git stash snapshots.
func (s *Server) handleRollbackWorktree(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req orchestrator.GitRollbackRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.WorktreePath) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "worktree_path is required"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if _, err := runGit(r.Context(), worktreePath, "reset", "--hard", "HEAD"); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to reset worktree: %v", err)})
		return
	}
	if _, err := runGit(r.Context(), worktreePath, "clean", "-fd"); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("failed to clean worktree: %v", err)})
		return
	}

	headRef, _ := gitHeadRef(r.Context(), worktreePath)
	writeJSON(w, http.StatusOK, orchestrator.GitRollbackResponse{
		TaskID:       req.TaskID,
		WorktreePath: worktreePath,
		RolledBack:   true,
		HeadRef:      headRef,
	})
}

// handleBrowserTest is intentionally disabled until LeVik has a constrained
// browser QA runner. Executing model-generated Node/Playwright scripts on the
// host is not acceptable for the native Mac daemon threat model.
func (s *Server) handleBrowserTest(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error": "browser QA requires a constrained runner and is not enabled in this host build",
	})
}

// ensureSnapshot verifies the managed worktree has a HEAD that rollback can
// reset to. A clean task worktree has no stashable changes before the first edit,
// so git stash is the wrong rollback primitive here.
func ensureSnapshot(ctx context.Context, worktreePath, taskID string) error {
	if _, err := runGit(ctx, worktreePath, "rev-parse", "--verify", "HEAD"); err != nil {
		return fmt.Errorf("failed to verify rollback base for task %s: %w", taskID, err)
	}
	return nil
}

// discoverLintCommands returns lint command candidates for the detected runtime.
func discoverLintCommands(worktreePath string, runtime string) []orchestrator.LintCommandCandidate {
	var candidates []orchestrator.LintCommandCandidate

	add := func(command, reason string) {
		command = strings.TrimSpace(command)
		if command == "" {
			return
		}
		candidates = append(candidates, orchestrator.LintCommandCandidate{
			Command:    command,
			WorkingDir: worktreePath,
			Runtime:    runtime,
			Reason:     reason,
		})
	}

	switch runtime {
	case "go":
		add("go vet ./...", "Go static analysis")
	case "python":
		if fileExists(filepath.Join(worktreePath, "pyproject.toml")) {
			add("ruff check .", "Python linter (ruff via pyproject.toml)")
		} else {
			add("ruff check .", "Python linter (ruff)")
		}
		add("flake8 .", "Python style checker (flake8)")
	case "node":
		add("eslint .", "JavaScript/TypeScript linter (eslint)")
	case "rust":
		add("cargo clippy -- -D warnings", "Rust linter (clippy)")
	default:
		// No language-specific lint tool; check for Makefile lint target
		if fileExists(filepath.Join(worktreePath, "Makefile")) {
			add("make lint", "Makefile lint target")
		}
	}

	return candidates
}

// handleDiscoverLint responds with available lint commands for a worktree.
func (s *Server) handleDiscoverLint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req orchestrator.LintDiscoveryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	runtime := detectRepoRuntime(worktreePath)
	candidates := discoverLintCommands(worktreePath, runtime)

	writeJSON(w, http.StatusOK, orchestrator.LintDiscoveryResponse{
		TaskID:       req.TaskID,
		WorktreePath: worktreePath,
		Runtime:      runtime,
		Candidates:   candidates,
	})
}

// handleRunLint executes a lint command and returns structured results.
// When a baseline is provided, only new errors (lines not in baseline) are returned.
func (s *Server) handleRunLint(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req orchestrator.LintRunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "command is required"})
		return
	}

	worktreePath, err := validatedManagedWorktreePath(s.cfg.WorkspaceRoot, req.WorktreePath)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	args := map[string]interface{}{
		"command":     req.Command,
		"working_dir": worktreePath,
	}
	result := s.execTool.Execute(r.Context(), tools.ToolContext{}, args)

	var exitCode int
	if result.ExitCode != nil {
		exitCode = int(*result.ExitCode)
	} else if result.IsError {
		exitCode = 1
	}

	newErrors := diffLintOutput(req.Baseline, result.ForLLM)

	writeJSON(w, http.StatusOK, orchestrator.LintRunResponse{
		TaskID:    req.TaskID,
		Command:   req.Command,
		Success:   !result.IsError,
		ExitCode:  exitCode,
		Output:    result.ForLLM,
		NewErrors: newErrors,
	})
}

// diffLintOutput compares current lint output against a baseline and returns
// only lines that are new (not present in the baseline).
func diffLintOutput(baseline, current string) []string {
	if baseline == "" {
		return nil // no baseline: caller gets full output, not diffed
	}
	baselineLines := strings.Split(baseline, "\n")
	baselineSet := make(map[string]struct{}, len(baselineLines))
	for _, line := range baselineLines {
		line = strings.TrimSpace(line)
		if line != "" {
			baselineSet[line] = struct{}{}
		}
	}
	var newErrors []string
	for _, line := range strings.Split(current, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if _, exists := baselineSet[line]; !exists {
				newErrors = append(newErrors, line)
			}
		}
	}
	return newErrors
}

// handleAgentRoster returns non-secret team routing metadata for the Python
// orchestrator. Secrets stay in Go provider config; Python only sees IDs,
// roles, provider names, models, and coarse capabilities.
func (s *Server) handleAgentRoster(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	agents := append([]orchestrator.AgentProfile(nil), s.cfg.AgentRoster...)
	writeJSON(w, http.StatusOK, orchestrator.AgentRosterResponse{Agents: agents})
}

// handleAgentThink executes a reasoning request selected by the Python orchestrator.
func (s *Server) handleAgentThink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.cfg.AgentThink == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "agent think not configured"})
		return
	}

	var req orchestrator.AgentThinkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Role) == "" || strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role and prompt are required"})
		return
	}

	resp, err := s.cfg.AgentThink(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("agent think failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleReviewChange asks the reviewer agent (a different model) to evaluate
// a set of code changes.  The review function is injected via Config so the
// gateway can wire the reviewer provider from the multi-agent registry.
func (s *Server) handleReviewChange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.cfg.ReviewChange == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "review function not configured"})
		return
	}

	var req orchestrator.ChangeReviewRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !taskIDPattern.MatchString(req.TaskID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "task_id contains unsupported characters"})
		return
	}
	if strings.TrimSpace(req.Objective) == "" || strings.TrimSpace(req.Diff) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "objective and diff are required"})
		return
	}

	resp, err := s.cfg.ReviewChange(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("review failed: %v", err)})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to remove non-socket path %s", path)
	}
	return os.Remove(path)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target interface{}) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxInboundBodyBytes)
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		return fmt.Errorf("invalid request body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

const obsMaxChars = 8000

// shapeObservation produces SWE-agent-style observation messages that guide
// the model toward better behaviour instead of just dumping raw output.
// Three templates: empty output, truncated output, normal output.
func shapeObservation(output string, isError bool) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		if isError {
			return "Your command failed and produced no output. Check the command syntax and try again."
		}
		return "Your command ran successfully and did not produce any output."
	}
	if len(trimmed) > obsMaxChars {
		elided := len(trimmed) - obsMaxChars
		preview := trimmed[:obsMaxChars]
		return fmt.Sprintf("Observation:\n%s\n<response clipped>\n<NOTE>%d more characters were elided. The output exceeded %d characters. To see the full output, redirect to a file or use head/tail/grep to narrow the result.</NOTE>",
			preview, elided, obsMaxChars)
	}
	return "Observation:\n" + trimmed
}

func summarizeObservation(output string, isError bool) string {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		if isError {
			return "command failed with no output"
		}
		return "command completed with no output"
	}
	lines := strings.Split(trimmed, "\n")
	summary := strings.TrimSpace(lines[0])
	if len(summary) > 160 {
		summary = summary[:160] + "..."
	}
	return summary
}

func isWithinRoot(candidate, root string) bool {
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(absRoot, absCandidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && rel != "..")
}

func runGit(ctx context.Context, repoPath string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func gitHeadRef(ctx context.Context, worktreePath string) (string, error) {
	output, err := runGit(ctx, worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitBranchName(ctx context.Context, worktreePath string) (string, error) {
	return gitHeadRef(ctx, worktreePath)
}

func gitCommitRef(ctx context.Context, worktreePath string) (string, error) {
	output, err := runGit(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

func gitStatusLines(ctx context.Context, worktreePath string) ([]string, error) {
	output, err := runGit(ctx, worktreePath, "status", "--short")
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil, nil
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) > 20 {
		lines = append(lines[:20], fmt.Sprintf("... %d more", len(lines)-20))
	}
	return lines, nil
}

func gitChangedFiles(ctx context.Context, worktreePath string, statusLines []string) ([]orchestrator.RepoChangedFile, int, int, string, error) {
	statusByPath := parseGitStatusByPath(statusLines)
	output, err := runGit(ctx, worktreePath, "diff", "--numstat", "HEAD", "--")
	if err != nil {
		return nil, 0, 0, "", err
	}
	shortStatOutput, err := runGit(ctx, worktreePath, "diff", "--shortstat", "HEAD", "--")
	if err != nil {
		return nil, 0, 0, "", err
	}

	changed := make([]orchestrator.RepoChangedFile, 0)
	seen := map[string]struct{}{}
	totalAdditions := 0
	totalDeletions := 0

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		path := normalizeGitPath(parts[len(parts)-1])
		if path == "" {
			continue
		}
		file := orchestrator.RepoChangedFile{
			Path:   path,
			Status: statusByPath[path],
		}
		if file.Status == "" {
			file.Status = "M"
		}
		if parts[0] == "-" || parts[1] == "-" {
			file.Binary = true
		} else {
			file.Additions, _ = strconv.Atoi(parts[0])
			file.Deletions, _ = strconv.Atoi(parts[1])
			totalAdditions += file.Additions
			totalDeletions += file.Deletions
		}
		changed = append(changed, file)
		seen[path] = struct{}{}
	}

	for _, line := range statusLines {
		path, status := parseGitStatusLine(line)
		if path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		changed = append(changed, orchestrator.RepoChangedFile{
			Path:   path,
			Status: status,
		})
	}

	sort.SliceStable(changed, func(i, j int) bool {
		return changed[i].Path < changed[j].Path
	})
	if len(changed) > 50 {
		changed = changed[:50]
	}
	return changed, totalAdditions, totalDeletions, strings.TrimSpace(shortStatOutput), nil
}

func parseGitStatusByPath(lines []string) map[string]string {
	result := map[string]string{}
	for _, line := range lines {
		path, status := parseGitStatusLine(line)
		if path != "" {
			result[path] = status
		}
	}
	return result
}

func parseGitStatusLine(line string) (string, string) {
	if len(line) < 4 {
		return "", ""
	}
	status := strings.TrimSpace(line[:2])
	path := strings.TrimSpace(line[3:])
	if path == "" {
		return "", ""
	}
	if strings.Contains(path, " -> ") {
		parts := strings.Split(path, " -> ")
		path = strings.TrimSpace(parts[len(parts)-1])
	}
	return normalizeGitPath(path), status
}

func normalizeGitPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, `"`)
	return filepath.ToSlash(path)
}

func topLevelEntries(worktreePath string) ([]string, error) {
	entries, err := os.ReadDir(worktreePath)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == ".git" {
			continue
		}
		if entry.IsDir() {
			name += "/"
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > 20 {
		names = append(names[:20], fmt.Sprintf("... %d more", len(names)-20))
	}
	return names, nil
}

func inspectKeyFiles(worktreePath string) ([]orchestrator.RepoFileSummary, error) {
	candidates := []string{
		"README.md",
		"README",
		"Makefile",
		"go.mod",
		"package.json",
		"pyproject.toml",
		"requirements.txt",
		"Cargo.toml",
		"docker-compose.yml",
		"docker-compose.yaml",
	}
	keyFiles := make([]orchestrator.RepoFileSummary, 0, len(candidates))
	for _, relativePath := range candidates {
		fullPath := filepath.Join(worktreePath, relativePath)
		info, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if info.IsDir() {
			continue
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, err
		}
		preview := string(data)
		if len(preview) > maxRepoPreviewBytes {
			preview = preview[:maxRepoPreviewBytes] + "\n... (truncated)"
		}
		keyFiles = append(keyFiles, orchestrator.RepoFileSummary{
			Path:    relativePath,
			Preview: preview,
			Bytes:   len(data),
		})
	}
	return keyFiles, nil
}

func normalizedArtifactFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "", "md", "markdown":
		return "md"
	case "json":
		return "json"
	case "txt", "text":
		return "txt"
	default:
		return "txt"
	}
}

func validatedManagedWorktreePath(workspaceRoot, candidate string) (string, error) {
	if strings.TrimSpace(candidate) == "" {
		return "", fmt.Errorf("worktree_path is required")
	}
	worktreePath, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid worktree_path")
	}
	worktreeRoot := filepath.Join(workspaceRoot, "worktrees")
	if !isWithinRoot(worktreePath, worktreeRoot) {
		return "", fmt.Errorf("worktree_path must remain inside the managed worktree root")
	}
	return worktreePath, nil
}

func resolveWorktreeFilePath(worktreePath, candidate string) (string, error) {
	var fullPath string
	if filepath.IsAbs(candidate) {
		fullPath = filepath.Clean(candidate)
	} else {
		fullPath = filepath.Join(worktreePath, candidate)
	}
	if !isWithinRoot(fullPath, worktreePath) {
		return "", fmt.Errorf("path must remain inside the managed worktree")
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	rel, relErr := filepath.Rel(worktreePath, absPath)
	if relErr == nil {
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for _, part := range parts {
			if part == ".git" {
				return "", fmt.Errorf("path must not target .git internals")
			}
		}
	}
	return absPath, nil
}

func resolveArtifactPath(artifactsRoot, candidate string) (string, error) {
	var fullPath string
	if filepath.IsAbs(candidate) {
		fullPath = filepath.Clean(candidate)
	} else {
		fullPath = filepath.Join(artifactsRoot, candidate)
	}
	if !isWithinRoot(fullPath, artifactsRoot) {
		return "", fmt.Errorf("path must remain inside the managed artifact root")
	}
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("artifact not found")
		}
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("path must be an artifact file")
	}
	return absPath, nil
}

func readBoundedFile(fullPath string, maxBytes int) (string, int, bool, error) {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return "", 0, false, err
	}
	truncated := false
	if len(data) > maxBytes {
		data = data[:maxBytes]
		truncated = true
	}
	return string(data), len(data), truncated, nil
}

func replaceUniqueText(fullPath, oldText, newText string) (int, error) {
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, fmt.Errorf("file not found")
		}
		return 0, fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return 0, fmt.Errorf("path must be a file")
	}

	content, err := os.ReadFile(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read file: %w", err)
	}
	contentStr := string(content)
	if !strings.Contains(contentStr, oldText) {
		return 0, fmt.Errorf("old_text was not found in the file")
	}
	if count := strings.Count(contentStr, oldText); count > 1 {
		return 0, fmt.Errorf("old_text appears %d times in the file; provide a more specific span", count)
	}

	updated := strings.Replace(contentStr, oldText, newText, 1)
	if err := os.WriteFile(fullPath, []byte(updated), info.Mode().Perm()); err != nil {
		return 0, fmt.Errorf("failed to write file: %w", err)
	}
	return len(updated), nil
}

func discoverRepoTargets(worktreePath, objective string, limit int) ([]orchestrator.RepoTargetCandidate, error) {
	tokens := objectiveTokens(objective)
	type candidate struct {
		path    string
		score   int
		reasons []string
	}
	candidates := make([]candidate, 0, limit*2)
	inspectedFiles := 0
	stopWalk := fmt.Errorf("stop-walk")

	err := filepath.WalkDir(worktreePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipRepoDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if inspectedFiles >= maxTargetDiscoveryFiles {
			return stopWalk
		}
		inspectedFiles++

		relPath, err := filepath.Rel(worktreePath, path)
		if err != nil {
			return nil
		}
		if !looksInspectableFile(path, relPath, d) {
			return nil
		}

		score, reasons, err := scoreRepoTarget(path, relPath, tokens)
		if err != nil {
			return nil
		}
		if score <= 0 {
			return nil
		}
		candidates = append(candidates, candidate{path: relPath, score: score, reasons: reasons})
		return nil
	})
	if err != nil && !errors.Is(err, stopWalk) {
		return nil, err
	}

	if len(candidates) == 0 {
		keyFiles, err := inspectKeyFiles(worktreePath)
		if err != nil {
			return nil, err
		}
		for _, item := range keyFiles {
			candidates = append(candidates, candidate{
				path:    item.Path,
				score:   1,
				reasons: []string{"fallback to key repository file"},
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score == candidates[j].score {
			return candidates[i].path < candidates[j].path
		}
		return candidates[i].score > candidates[j].score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	result := make([]orchestrator.RepoTargetCandidate, 0, len(candidates))
	for _, item := range candidates {
		result = append(result, orchestrator.RepoTargetCandidate{
			Path:   item.path,
			Score:  item.score,
			Reason: strings.Join(uniqueStrings(item.reasons), "; "),
		})
	}
	return result, nil
}

func discoverVerificationCommands(worktreePath string, targetPaths []string) (string, []orchestrator.VerificationCommandCandidate, error) {
	runtime := detectRepoRuntime(worktreePath)
	candidates := make([]orchestrator.VerificationCommandCandidate, 0, defaultVerificationLimit)
	seen := map[string]struct{}{}

	addCandidate := func(command, reason string) {
		command = strings.TrimSpace(command)
		if command == "" {
			return
		}
		if _, exists := seen[command]; exists {
			return
		}
		seen[command] = struct{}{}
		candidates = append(candidates, orchestrator.VerificationCommandCandidate{
			Command:    command,
			WorkingDir: worktreePath,
			Runtime:    runtime,
			Reason:     reason,
		})
	}

	switch runtime {
	case "go":
		for _, target := range targetPaths {
			if pkg := goTestPackageForPath(target); pkg != "" {
				addCandidate("go test "+pkg, fmt.Sprintf("target-scoped Go package for `%s`", target))
			}
		}
		addCandidate("go test ./...", "full Go repository verification")
	case "python":
		for _, target := range targetPaths {
			if strings.HasSuffix(target, "_test.py") || strings.Contains(target, "/tests/") || strings.HasPrefix(target, "tests/") {
				addCandidate("pytest "+target, fmt.Sprintf("target-specific pytest path for `%s`", target))
			}
		}
		addCandidate("pytest", "default Python test command")
	case "node":
		addCandidate("npm test", "default Node test command from package.json")
	case "rust":
		addCandidate("cargo test", "default Rust test command")
	default:
		if fileExists(filepath.Join(worktreePath, "Makefile")) {
			addCandidate("make test", "Makefile-based test target")
		}
	}

	if fileExists(filepath.Join(worktreePath, "Makefile")) && runtime != "node" {
		addCandidate("make test", "Makefile-based test target")
	}
	if len(candidates) > defaultVerificationLimit {
		candidates = candidates[:defaultVerificationLimit]
	}
	return runtime, candidates, nil
}

func scoreRepoTarget(path, relPath string, tokens []string) (int, []string, error) {
	if len(tokens) == 0 {
		return 0, nil, nil
	}
	content, _, _, err := readBoundedFile(path, 4096)
	if err != nil {
		return 0, nil, err
	}
	if !looksText(content) {
		return 0, nil, nil
	}

	lowerPath := strings.ToLower(relPath)
	lowerContent := strings.ToLower(content)
	score := 0
	reasons := []string{}
	for _, token := range tokens {
		if strings.Contains(lowerPath, token) {
			score += 5
			reasons = append(reasons, fmt.Sprintf("path matched `%s`", token))
		}
		if strings.Contains(lowerContent, token) {
			score += 3
			reasons = append(reasons, fmt.Sprintf("content matched `%s`", token))
		}
	}
	if strings.HasSuffix(lowerPath, "/test") || strings.Contains(lowerPath, "test") {
		score += 1
		reasons = append(reasons, "testing-related path")
	}
	return score, reasons, nil
}

func objectiveTokens(objective string) []string {
	raw := objectiveTokenPattern.FindAllString(strings.ToLower(objective), -1)
	seen := map[string]struct{}{}
	stopwords := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {}, "from": {},
		"into": {}, "your": {}, "their": {}, "have": {}, "will": {}, "should": {}, "need": {},
		"make": {}, "best": {}, "possible": {}, "agent": {}, "team": {}, "enterprise": {},
	}
	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) < 3 {
			continue
		}
		if _, blocked := stopwords[token]; blocked {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}
	return tokens
}

func detectRepoRuntime(worktreePath string) string {
	switch {
	case fileExists(filepath.Join(worktreePath, "go.mod")):
		return "go"
	case fileExists(filepath.Join(worktreePath, "package.json")):
		return "node"
	case fileExists(filepath.Join(worktreePath, "pyproject.toml")) || fileExists(filepath.Join(worktreePath, "requirements.txt")):
		return "python"
	case fileExists(filepath.Join(worktreePath, "Cargo.toml")):
		return "rust"
	default:
		return ""
	}
}

func goTestPackageForPath(target string) string {
	cleaned := filepath.Clean(strings.TrimSpace(target))
	if cleaned == "." || cleaned == "" {
		return ""
	}
	dir := cleaned
	if filepath.Ext(cleaned) != "" {
		dir = filepath.Dir(cleaned)
	}
	if dir == "." {
		return "./..."
	}
	return "./" + filepath.ToSlash(dir)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func shouldSkipRepoDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "coverage", "__pycache__", ".venv", "vendor":
		return true
	default:
		return false
	}
}

func looksInspectableFile(fullPath, relPath string, entry fs.DirEntry) bool {
	if relPath == ".git" {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(relPath), "/") {
		if part == ".git" {
			return false
		}
	}
	info, err := entry.Info()
	if err != nil {
		return false
	}
	if info.Size() > maxTargetFileBytes {
		return false
	}
	ext := strings.ToLower(filepath.Ext(relPath))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".pdf", ".zip", ".tar", ".gz", ".class", ".jar", ".so", ".dylib", ".exe", ".bin", ".mp4", ".mp3", ".wav":
		return false
	}
	return true
}

func looksText(content string) bool {
	for _, r := range content {
		if r == 0 {
			return false
		}
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return false
		}
	}
	return true
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

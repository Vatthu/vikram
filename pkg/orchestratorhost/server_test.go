package orchestratorhost

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/v1claw/levik/pkg/orchestrator"
	"github.com/v1claw/levik/pkg/tools"
)

type stubNotifier struct {
	channel string
	chatID  string
	content string
}

func (s *stubNotifier) SendToChannel(ctx context.Context, channelName, chatID, content string) error {
	s.channel = channelName
	s.chatID = chatID
	s.content = content
	return nil
}

type stubExecTool struct {
	result *tools.ToolResult
	args   map[string]interface{}
}

func (s *stubExecTool) Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult {
	s.args = args
	return s.result
}

func TestProvisionWorkspaceCreatesTaskLayout(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		SocketPath:          filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot:       root,
		RestrictToWorkspace: true,
	}, nil)

	reqBody, err := json.Marshal(orchestrator.WorkspaceProvisionRequest{
		TaskID: "task_123",
		Repo: orchestrator.RepoRef{
			Path:          "/tmp/repo",
			DefaultBranch: "main",
		},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/provision", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.WorkspaceProvisionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "task_123", resp.TaskID)
	require.DirExists(t, resp.ArtifactsDir)
	require.DirExists(t, resp.LogsDir)
	require.DirExists(t, resp.ScratchDir)
	require.Equal(t, filepath.Join(root, "worktrees", "task_123"), resp.WorktreePath)
}

func TestExecEndpointUsesConfiguredExecutor(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	server.execTool = &stubExecTool{
		result: tools.NewToolResult("hello from exec"),
	}

	reqBody, err := json.Marshal(orchestrator.HostActionRequest{
		TaskID:     "task_123",
		ActionName: "exec",
		WorkingDir: root,
		Arguments:  map[string]interface{}{"command": "pwd"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/exec", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.HostObservation
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Success)
	require.Equal(t, "task_123", resp.TaskID)
	require.Equal(t, "exec", resp.ActionName)
	require.Contains(t, resp.Output, "hello from exec")
	require.Equal(t, root, resp.State["working_dir"])
}

func TestNotifyTelegramUsesNotifier(t *testing.T) {
	root := t.TempDir()
	notifier := &stubNotifier{}
	server := NewServer(Config{
		SocketPath:      filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot:   root,
		TelegramEnabled: true,
	}, notifier)

	reqBody, err := json.Marshal(orchestrator.ChannelNotificationRequest{
		ChatID:  "123456",
		Content: "phase complete",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/notify/telegram", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "telegram", notifier.channel)
	require.Equal(t, "123456", notifier.chatID)
	require.Equal(t, "phase complete", notifier.content)
}

func TestAgentRosterEndpointReturnsNonSecretRoutingMetadata(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
		AgentRoster: []orchestrator.AgentProfile{
			{
				ID:           "lead-1",
				Name:         "Lead",
				Role:         "lead",
				ProviderName: "zhipu",
				Model:        "glm-5.1",
				Capabilities: []string{"planning", "architecture"},
			},
			{
				ID:           "reviewer-1",
				Role:         "reviewer",
				ProviderName: "deepseek",
				Model:        "deepseek-reasoner",
				Capabilities: []string{"review", "adversarial"},
			},
		},
	}, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/agent/roster", nil)
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.AgentRosterResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Agents, 2)
	require.Equal(t, "lead", resp.Agents[0].Role)
	require.Equal(t, "zhipu", resp.Agents[0].ProviderName)
	require.Equal(t, "glm-5.1", resp.Agents[0].Model)
	require.Contains(t, resp.Agents[1].Capabilities, "adversarial")
}

func TestCreateWorktreeCreatesManagedWorktree(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)

	reqBody, err := json.Marshal(orchestrator.GitWorktreeCreateRequest{
		TaskID:       "task_123",
		Repo:         orchestrator.RepoRef{Path: repoPath, DefaultBranch: "main"},
		WorktreePath: filepath.Join(root, "worktrees", "task_123"),
		Branch:       "levik/task_123",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/git/worktrees/create", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.GitWorktreeCreateResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Created)
	require.Equal(t, "levik/task_123", resp.HeadRef)
	require.FileExists(t, filepath.Join(resp.WorktreePath, "README.md"))
}

func TestWriteArtifactPersistsContent(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)

	reqBody, err := json.Marshal(orchestrator.ArtifactWriteRequest{
		Artifact: orchestrator.Artifact{
			TaskID:     "task_123",
			ArtifactID: "plan-initial",
			Kind:       orchestrator.ArtifactKindPlan,
			Title:      "Initial Plan",
			Summary:    "Plan artifact created",
		},
		Content: "# Initial Plan\n",
		Format:  "markdown",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/write", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.ArtifactWriteResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "plan-initial", resp.Artifact.ArtifactID)
	require.FileExists(t, resp.Path)

	data, readErr := os.ReadFile(resp.Path)
	require.NoError(t, readErr)
	require.Equal(t, "# Initial Plan\n", string(data))
}

func TestReadArtifactReturnsBoundedContent(t *testing.T) {
	root := t.TempDir()
	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)

	artifactPath := filepath.Join(root, "tasks", "task_123", "artifacts", "merge-readiness-1.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(artifactPath), 0o755))
	require.NoError(t, os.WriteFile(artifactPath, []byte("# Merge Readiness\nready\n"), 0o644))

	reqBody, err := json.Marshal(orchestrator.ArtifactReadRequest{
		TaskID:   "task_123",
		Path:     artifactPath,
		MaxBytes: 18,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/artifacts/read", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.ArtifactReadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "task_123", resp.TaskID)
	require.Equal(t, artifactPath, resp.Path)
	require.True(t, resp.Truncated)
	require.Equal(t, 18, resp.BytesRead)
	require.Equal(t, "# Merge Readiness\n", resp.Content)
}

func TestRemoveWorktreeRemovesManagedWorktree(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.GitWorktreeRemoveRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Force:        true,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/git/worktrees/remove", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.GitWorktreeRemoveResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, resp.Removed)
	_, statErr := os.Stat(worktreePath)
	require.True(t, os.IsNotExist(statErr))
}

func TestInspectRepoReturnsBoundedSummary(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("# Example Repo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte("module example.com/repo\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md", "go.mod")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")
	require.NoError(t, os.WriteFile(filepath.Join(worktreePath, "README.md"), []byte("# Example Repo\n\nupdated\n"), 0o644))

	reqBody, err := json.Marshal(orchestrator.RepoInspectRequest{
		TaskID:       "task_123",
		RepoPath:     repoPath,
		WorktreePath: worktreePath,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/repos/inspect", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.RepoInspectResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "levik/task_123", resp.Branch)
	require.True(t, resp.Dirty)
	require.GreaterOrEqual(t, resp.ChangedFileCount, 1)
	require.GreaterOrEqual(t, resp.Additions, 1)
	require.Contains(t, resp.DiffShortStat, "insertions")
	changedPaths := make([]string, 0, len(resp.ChangedFiles))
	for _, file := range resp.ChangedFiles {
		changedPaths = append(changedPaths, file.Path)
	}
	require.Contains(t, changedPaths, "README.md")
	require.Contains(t, resp.TopLevelEntries, "README.md")
	require.Len(t, resp.KeyFiles, 2)
	require.Equal(t, "README.md", resp.KeyFiles[0].Path)
}

func TestDiscoverTargetsReturnsScoredCandidates(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "pkg"), 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("workflow plan\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "pkg", "workflow.go"), []byte("package pkg\n// workflow plan\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md", "pkg/workflow.go")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.RepoTargetDiscoveryRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Objective:    "improve workflow plan",
		Limit:        3,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/repos/discover-targets", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.RepoTargetDiscoveryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Candidates)
	require.Equal(t, "pkg/workflow.go", resp.Candidates[0].Path)
	require.Greater(t, resp.Candidates[0].Score, 0)
}

func TestReadFileReturnsBoundedContent(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("1234567890"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.FileReadRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Path:         "README.md",
		MaxBytes:     4,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/files/read", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.FileReadResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "README.md", resp.Path)
	require.Equal(t, "1234", resp.Content)
	require.True(t, resp.Truncated)
}

func TestLooksInspectableFileSkipsGitMetadataFiles(t *testing.T) {
	root := t.TempDir()
	gitFile := filepath.Join(root, ".git")
	require.NoError(t, os.WriteFile(gitFile, []byte("gitdir: /tmp/example\n"), 0o644))

	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.False(t, looksInspectableFile(gitFile, ".git", entries[0]))
	require.False(t, looksInspectableFile(filepath.Join(root, ".git", "config"), ".git/config", entries[0]))
}

func TestWriteFileWritesInsideManagedWorktree(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("hello"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.FileWriteRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Path:         "notes/edit.txt",
		Content:      "bounded write",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/files/write", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.FileWriteResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "notes/edit.txt", resp.Path)
	data, readErr := os.ReadFile(filepath.Join(worktreePath, "notes", "edit.txt"))
	require.NoError(t, readErr)
	require.Equal(t, "bounded write", string(data))
}

func TestReplaceFileUpdatesUniqueSpanInsideManagedWorktree(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("alpha\nbeta\ngamma\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.FileReplaceRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Path:         "README.md",
		OldText:      "beta",
		NewText:      "delta",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/files/replace", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.FileReplaceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "README.md", resp.Path)

	data, readErr := os.ReadFile(filepath.Join(worktreePath, "README.md"))
	require.NoError(t, readErr)
	require.Equal(t, "alpha\ndelta\ngamma\n", string(data))
}

func TestReplaceFileRejectsAmbiguousSpan(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(repoPath, 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "README.md"), []byte("repeat\nrepeat\n"), 0o644))
	runGitCommand(t, repoPath, "add", "README.md")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.FileReplaceRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		Path:         "README.md",
		OldText:      "repeat",
		NewText:      "done",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/files/replace", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "appears 2 times")
}

func TestDiscoverVerificationReturnsFocusedCandidates(t *testing.T) {
	root := t.TempDir()
	repoPath := filepath.Join(root, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repoPath, "pkg", "sample"), 0o755))
	runGitCommand(t, repoPath, "init")
	runGitCommand(t, repoPath, "config", "user.name", "LeVik Test")
	runGitCommand(t, repoPath, "config", "user.email", "levik@example.com")
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "go.mod"), []byte("module example.com/repo\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, "pkg", "sample", "service.go"), []byte("package sample\n"), 0o644))
	runGitCommand(t, repoPath, "add", "go.mod", "pkg/sample/service.go")
	runGitCommand(t, repoPath, "commit", "-m", "initial")
	runGitCommand(t, repoPath, "branch", "-M", "main")

	server := NewServer(Config{
		SocketPath:    filepath.Join(root, "run", "levikd.sock"),
		WorkspaceRoot: root,
	}, nil)
	worktreePath := filepath.Join(root, "worktrees", "task_123")
	runGitCommand(t, repoPath, "worktree", "add", "-b", "levik/task_123", worktreePath, "main")

	reqBody, err := json.Marshal(orchestrator.VerificationDiscoveryRequest{
		TaskID:       "task_123",
		WorktreePath: worktreePath,
		TargetPaths:  []string{"pkg/sample/service.go"},
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v1/repos/discover-verification", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp orchestrator.VerificationDiscoveryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "go", resp.Runtime)
	require.NotEmpty(t, resp.Candidates)
	require.Equal(t, "go test ./pkg/sample", resp.Candidates[0].Command)
}

func runGitCommand(t *testing.T, repoPath string, args ...string) {
	t.Helper()
	commandArgs := append([]string{"-C", repoPath}, args...)
	cmd := exec.Command("git", commandArgs...)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}

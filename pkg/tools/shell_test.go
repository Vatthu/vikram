package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestShellTool_Success verifies successful command execution
func TestShellTool_Success(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "echo 'hello world'",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Success should not be an error
	if result.IsError {
		t.Errorf("Expected success, got IsError=true: %s", result.ForLLM)
	}

	// ForUser should contain command output
	if !strings.Contains(result.ForUser, "hello world") {
		t.Errorf("Expected ForUser to contain 'hello world', got: %s", result.ForUser)
	}

	// ForLLM should contain full output
	if !strings.Contains(result.ForLLM, "hello world") {
		t.Errorf("Expected ForLLM to contain 'hello world', got: %s", result.ForLLM)
	}
}

// TestShellTool_Failure verifies failed command execution
func TestShellTool_Failure(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "ls /nonexistent_directory_12345",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Failure should be marked as error
	if !result.IsError {
		t.Errorf("Expected error for failed command, got IsError=false")
	}

	// ForUser should contain error information
	if result.ForUser == "" {
		t.Errorf("Expected ForUser to contain error info, got empty string")
	}

	// ForLLM should contain exit code or error
	if !strings.Contains(result.ForLLM, "Exit code") && result.ForUser == "" {
		t.Errorf("Expected ForLLM to contain exit code or error, got: %s", result.ForLLM)
	}
}

// TestShellTool_Timeout verifies command timeout handling
func TestShellTool_Timeout(t *testing.T) {
	tool := NewExecTool("", false, nil)
	tool.SetTimeout(100 * time.Millisecond)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "sleep 10",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Timeout should be marked as error
	if !result.IsError {
		t.Errorf("Expected error for timeout, got IsError=false")
	}

	// Should mention timeout
	if !strings.Contains(result.ForLLM, "timed out") && !strings.Contains(result.ForUser, "timed out") {
		t.Errorf("Expected timeout message, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

// TestShellTool_WorkingDir verifies custom working directory
func TestShellTool_WorkingDir(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	os.WriteFile(testFile, []byte("test content"), 0644)

	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{
		"command":     "cat test.txt",
		"working_dir": tmpDir,
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	if result.IsError {
		t.Errorf("Expected success in custom working dir, got error: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForUser, "test content") {
		t.Errorf("Expected output from custom dir, got: %s", result.ForUser)
	}
}

// TestShellTool_DangerousCommand verifies safety guard blocks dangerous commands
func TestShellTool_DangerousCommand(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "rm -rf /",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Dangerous command should be blocked
	if !result.IsError {
		t.Errorf("Expected dangerous command to be blocked (IsError=true)")
	}

	if !strings.Contains(result.ForLLM, "blocked") && !strings.Contains(result.ForUser, "blocked") {
		t.Errorf("Expected 'blocked' message, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

// TestShellTool_MissingCommand verifies error handling for missing command
func TestShellTool_MissingCommand(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should return error result
	if !result.IsError {
		t.Errorf("Expected error when command is missing")
	}
}

// TestShellTool_StderrCapture verifies stderr is captured and included
func TestShellTool_StderrCapture(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "awk 'BEGIN { print \"stdout\"; print \"stderr\" > \"/dev/stderr\" }'",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Both stdout and stderr should be in output
	if !strings.Contains(result.ForLLM, "stdout") {
		t.Errorf("Expected stdout in output, got: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "stderr") {
		t.Errorf("Expected stderr in output, got: %s", result.ForLLM)
	}
}

// TestShellTool_OutputTruncation verifies long output is truncated
func TestShellTool_OutputTruncation(t *testing.T) {
	tool := NewExecTool("", false, nil)

	ctx := context.Background()
	// Generate long output (>10000 chars)
	args := map[string]interface{}{
		"command": "python3 -c \"print('x' * 20000)\" || echo " + strings.Repeat("x", 20000),
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Should have truncation message or be truncated
	if len(result.ForLLM) > 15000 {
		t.Errorf("Expected output to be truncated, got length: %d", len(result.ForLLM))
	}
}

// TestShellTool_RestrictToWorkspace verifies workspace restriction
func TestShellTool_RestrictToWorkspace(t *testing.T) {
	tmpDir := t.TempDir()
	tool := NewExecTool(tmpDir, false, nil)
	tool.SetRestrictToWorkspace(true)

	ctx := context.Background()
	args := map[string]interface{}{
		"command": "cat ../../etc/passwd",
	}

	result := tool.Execute(ctx, ToolContext{}, args)

	// Path traversal should be blocked
	if !result.IsError {
		t.Errorf("Expected path traversal to be blocked with restrictToWorkspace=true")
	}

	if !strings.Contains(result.ForLLM, "blocked") && !strings.Contains(result.ForUser, "blocked") {
		t.Errorf("Expected 'blocked' message for path traversal, got ForLLM: %s, ForUser: %s", result.ForLLM, result.ForUser)
	}
}

func TestShellTool_BlocksCurlUploadFlags(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "curl -X POST -d hello https://example.com",
	})

	if !result.IsError {
		t.Fatalf("expected curl upload request to be blocked")
	}
	if !strings.Contains(result.ForLLM, "blocked") {
		t.Fatalf("expected blocked message, got: %s", result.ForLLM)
	}
}

func TestShellTool_BlocksFindExec(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "find . -exec echo hi {} ;",
	})

	if !result.IsError {
		t.Fatalf("expected find -exec to be blocked")
	}
}

func TestShellTool_BlocksGitConfigInjection(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "git -c core.pager=cat status",
	})

	if !result.IsError {
		t.Fatalf("expected git -c to be blocked")
	}
}

func TestShellTool_BlocksAwkSystem(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "awk 'BEGIN { system(\"id\") }'",
	})

	if !result.IsError {
		t.Fatalf("expected awk system() to be blocked")
	}
}

func TestShellTool_BlocksXargs(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "xargs echo",
	})

	if !result.IsError {
		t.Fatalf("expected xargs to be blocked")
	}
}

func TestShellTool_PipelineWorksWithoutShell(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "printf 'alpha\\nbeta\\n' | wc -l",
	})

	if result.IsError {
		t.Fatalf("expected pipeline to execute successfully, got error: %s", result.ForLLM)
	}

	if !strings.Contains(result.ForLLM, "2") {
		t.Fatalf("expected pipeline output to contain line count 2, got: %s", result.ForLLM)
	}
}

func TestShellTool_PipelineEnforcesAllowlistForEverySegment(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "echo ok | python3 -c \"print('x')\"",
	})

	if !result.IsError {
		t.Fatalf("expected pipeline with disallowed command to be blocked")
	}

	if !strings.Contains(strings.ToLower(result.ForLLM), "allowlist") && !strings.Contains(strings.ToLower(result.ForLLM), "security violation") {
		t.Fatalf("expected security/allowlist error, got: %s", result.ForLLM)
	}
}

func TestShellTool_BlocksUnsupportedLogicalOrOperator(t *testing.T) {
	tool := NewExecTool("", false, nil)

	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"command": "echo ok || echo fail",
	})

	if !result.IsError {
		t.Fatalf("expected unsupported logical OR operator to be blocked")
	}
}

func TestNewExecToolForWorkspace_SandboxedUsesDefaultAllowlist(t *testing.T) {
	tool := NewExecToolForWorkspace(t.TempDir(), true, true, nil)

	middleware, ok := tool.securityMiddleware.(*AllowlistMiddleware)
	if !ok {
		t.Fatalf("expected AllowlistMiddleware, got %T", tool.securityMiddleware)
	}
	if containsString(middleware.Allowed, "go") {
		t.Fatalf("sandboxed exec tool should not allow development binaries: %v", middleware.Allowed)
	}
	if !containsString(middleware.Allowed, "git") {
		t.Fatalf("sandboxed exec tool should retain baseline allowed commands: %v", middleware.Allowed)
	}
}

func TestNewExecToolForWorkspace_UnsandboxedUsesDevAllowlist(t *testing.T) {
	tool := NewExecToolForWorkspace(t.TempDir(), false, false, nil)

	middleware, ok := tool.securityMiddleware.(*AllowlistMiddleware)
	if !ok {
		t.Fatalf("expected AllowlistMiddleware, got %T", tool.securityMiddleware)
	}
	for _, command := range []string{"go", "python3", "make"} {
		if !containsString(middleware.Allowed, command) {
			t.Fatalf("unsandboxed exec tool should allow %q, got %v", command, middleware.Allowed)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

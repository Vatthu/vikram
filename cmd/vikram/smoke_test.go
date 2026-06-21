package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSmokeLLMReplyRoutesReviewerPrompt(t *testing.T) {
	reply := smokeLLMReply(`Respond with ONLY a JSON object:
{
  "verdict": "APPROVE" | "CHANGES_REQUESTED" | "REJECT"
}`)

	if !strings.Contains(reply, `"verdict":"APPROVE"`) {
		t.Fatalf("review reply = %q, want APPROVE JSON", reply)
	}
}

func TestSmokeLLMReplyRoutesRunnerPrompt(t *testing.T) {
	reply := smokeLLMReply(`Respond with a JSON object:
{"verdict": "PASSED" or "FAILED", "summary": "brief explanation", "issues": ["issue1"]}`)

	if !strings.Contains(reply, `"verdict":"PASSED"`) {
		t.Fatalf("runner reply = %q, want PASSED JSON", reply)
	}
}

func TestBuildSmokeConfigUsesLocalVLLMTeam(t *testing.T) {
	cfg := buildSmokeConfig("/tmp/vikram-smoke-workspace", "http://127.0.0.1:18080/v1", 18990)

	if cfg.Agents.Defaults.Provider != "vllm" {
		t.Fatalf("provider = %q, want vllm", cfg.Agents.Defaults.Provider)
	}
	if cfg.Providers.VLLM.APIBase != "http://127.0.0.1:18080/v1" {
		t.Fatalf("api base = %q", cfg.Providers.VLLM.APIBase)
	}
	if got := len(cfg.Agents.List); got != 4 {
		t.Fatalf("agent count = %d, want 4", got)
	}
	if cfg.Heartbeat.Enabled {
		t.Fatal("heartbeat should be disabled for smoke config")
	}
}

func TestSmokeRepoPathStaysInsideWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	repoPath := smokeRepoPath(workspace)

	rel, err := filepath.Rel(workspace, repoPath)
	if err != nil {
		t.Fatalf("rel: %v", err)
	}
	if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("repo path %q must stay inside workspace %q", repoPath, workspace)
	}
}

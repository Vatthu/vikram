package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/v1claw/levik/pkg/config"
	"github.com/v1claw/levik/pkg/providers"
)

func TestBuildMessages_SkipsEmptyCurrentMessage(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)

	history := []providers.Message{
		{Role: "assistant", Content: "hello"},
	}

	messages := cb.BuildMessages(history, "", "", nil, "", "")
	require.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0].Role)
	assert.Equal(t, "assistant", messages[1].Role)
}

func TestResolveBuiltinSkillsDir_PrefersWorkspaceSkills(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills")
	require.NoError(t, os.MkdirAll(skillsDir, 0755))

	resolved := resolveBuiltinSkillsDir(workspace)
	assert.Equal(t, skillsDir, resolved)
}

func TestResolveBuiltinSkillsDir_FallsBackToGlobalSkills(t *testing.T) {
	workspace := t.TempDir()
	home := t.TempDir()
	t.Setenv(config.HomeEnvVar, home)

	globalSkillsDir := filepath.Join(home, "skills")
	require.NoError(t, os.MkdirAll(globalSkillsDir, 0755))

	resolved := resolveBuiltinSkillsDir(workspace)
	assert.Equal(t, globalSkillsDir, resolved)
}

func TestLoadBootstrapFiles_PrefersAgentsFileAndIncludesBootstrapFiles(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte("agents"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "AGENT.md"), []byte("agent"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "BOOTSTRAP.md"), []byte("bootstrap"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "SOUL.md"), []byte("soul"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "USER.md"), []byte("user"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "IDENTITY.md"), []byte("identity"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "TOOLS.md"), []byte("tools"), 0644))

	cb := NewContextBuilder(workspace)
	content := cb.LoadBootstrapFiles()

	assert.Contains(t, content, `filename="AGENTS.md"`)
	assert.Contains(t, content, "agents")
	assert.NotContains(t, content, `filename="AGENT.md"`)
	assert.Contains(t, content, `filename="BOOTSTRAP.md"`)
	assert.Contains(t, content, `filename="TOOLS.md"`)
	assert.NotContains(t, content, "<missing_workspace_file")
}

func TestBuildSystemPrompt_IncludesBootstrapGuidance(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "BOOTSTRAP.md"), []byte("pending"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "AGENT.md"), []byte("agent"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "SOUL.md"), []byte("soul"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "USER.md"), []byte("user"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "IDENTITY.md"), []byte("identity"), 0644))

	cb := NewContextBuilder(workspace)
	prompt := cb.BuildSystemPrompt()

	assert.Contains(t, prompt, "BOOTSTRAP.md is present")
	assert.Contains(t, prompt, "complete_bootstrap")
}

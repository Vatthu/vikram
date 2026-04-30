package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapTool_CompletesBootstrapAndRemovesMarker(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "BOOTSTRAP.md"), []byte("pending"), 0644))

	tool := NewBootstrapTool(workspace)
	result := tool.Execute(context.Background(), ToolContext{}, map[string]interface{}{
		"assistant_name":   "Jarvis",
		"user_name":        "Alex",
		"relationship":     "Alex's personal AI assistant",
		"role":             "A practical AI operating partner",
		"tone":             "Direct, calm, and grounded",
		"priorities":       "Help Alex finish technical work with minimal fluff",
		"boundaries":       "Be explicit about failures and limits",
		"user_preferences": "Keep replies concise",
	})

	require.False(t, result.IsError, result.ForLLM)
	assert.Contains(t, result.ForLLM, "Bootstrap completed for Jarvis")

	_, err := os.Stat(filepath.Join(workspace, "BOOTSTRAP.md"))
	assert.True(t, os.IsNotExist(err))

	identityData, err := os.ReadFile(filepath.Join(workspace, "IDENTITY.md"))
	require.NoError(t, err)
	assert.Contains(t, string(identityData), "Jarvis")
	assert.Contains(t, string(identityData), "Alex's personal AI assistant")

	userData, err := os.ReadFile(filepath.Join(workspace, "USER.md"))
	require.NoError(t, err)
	assert.Contains(t, string(userData), "Alex")

	memoryData, err := os.ReadFile(filepath.Join(workspace, "memory", "MEMORY.md"))
	require.NoError(t, err)
	assert.Contains(t, string(memoryData), "Bootstrap completed successfully")
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_ReadToday_PrefersLegacyFile(t *testing.T) {
	workspace := t.TempDir()
	ms := NewMemoryStore(workspace)

	today := time.Now()
	legacyFile := filepath.Join(workspace, "memory", today.Format("2006-01-02")+".md")
	nestedFile := filepath.Join(workspace, "memory", today.Format("200601"), today.Format("20060102")+".md")

	require.NoError(t, os.MkdirAll(filepath.Dir(nestedFile), 0755))
	require.NoError(t, os.WriteFile(nestedFile, []byte("nested"), 0644))
	require.NoError(t, os.WriteFile(legacyFile, []byte("legacy"), 0644))

	assert.Equal(t, "legacy", ms.ReadToday())
}

func TestMemoryStore_AppendToday_UsesLegacyWhenWorkspaceUsesLegacyFormat(t *testing.T) {
	workspace := t.TempDir()
	ms := NewMemoryStore(workspace)

	yesterday := time.Now().AddDate(0, 0, -1)
	yesterdayLegacy := filepath.Join(workspace, "memory", yesterday.Format("2006-01-02")+".md")
	require.NoError(t, os.WriteFile(yesterdayLegacy, []byte("# yesterday\n"), 0644))

	require.NoError(t, ms.AppendToday("hello"))

	today := time.Now()
	todayLegacy := filepath.Join(workspace, "memory", today.Format("2006-01-02")+".md")
	nestedToday := filepath.Join(workspace, "memory", today.Format("200601"), today.Format("20060102")+".md")

	data, err := os.ReadFile(todayLegacy)
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello")

	_, err = os.Stat(nestedToday)
	assert.True(t, os.IsNotExist(err))
}

func TestMemoryStore_GetRecentDailyNotes_ReadsLegacyAndNestedFormats(t *testing.T) {
	workspace := t.TempDir()
	ms := NewMemoryStore(workspace)

	today := time.Now()
	todayLegacy := filepath.Join(workspace, "memory", today.Format("2006-01-02")+".md")
	require.NoError(t, os.WriteFile(todayLegacy, []byte("legacy today"), 0644))

	yesterday := today.AddDate(0, 0, -1)
	yesterdayNested := filepath.Join(workspace, "memory", yesterday.Format("200601"), yesterday.Format("20060102")+".md")
	require.NoError(t, os.MkdirAll(filepath.Dir(yesterdayNested), 0755))
	require.NoError(t, os.WriteFile(yesterdayNested, []byte("nested yesterday"), 0644))

	notes := ms.GetRecentDailyNotes(2)
	assert.Contains(t, notes, "legacy today")
	assert.Contains(t, notes, "nested yesterday")
}

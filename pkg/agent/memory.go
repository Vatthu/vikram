// License: MIT

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MemoryStore manages persistent memory for the agent.
// - Long-term memory: memory/MEMORY.md
// - Daily notes: memory/YYYY-MM-DD.md (legacy) or memory/YYYYMM/YYYYMMDD.md
type MemoryStore struct {
	workspace  string
	memoryDir  string
	memoryFile string
}

// NewMemoryStore creates a new MemoryStore with the given workspace path.
// It ensures the memory directory exists.
func NewMemoryStore(workspace string) *MemoryStore {
	memoryDir := filepath.Join(workspace, "memory")
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")

	if err := os.MkdirAll(memoryDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to create memory directory: %v\n", err)
	}

	return &MemoryStore{
		workspace:  workspace,
		memoryDir:  memoryDir,
		memoryFile: memoryFile,
	}
}

// getTodayFile returns the nested daily note path (memory/YYYYMM/YYYYMMDD.md).
func (ms *MemoryStore) getTodayFile() string {
	return ms.getDailyFileNested(time.Now())
}

func (ms *MemoryStore) getTodayFileLegacy() string {
	return ms.getDailyFileLegacy(time.Now())
}

func (ms *MemoryStore) getDailyFileNested(date time.Time) string {
	dateStr := date.Format("20060102") // YYYYMMDD
	monthDir := dateStr[:6]            // YYYYMM
	return filepath.Join(ms.memoryDir, monthDir, dateStr+".md")
}

func (ms *MemoryStore) getDailyFileLegacy(date time.Time) string {
	dateStr := date.Format("2006-01-02") // YYYY-MM-DD
	return filepath.Join(ms.memoryDir, dateStr+".md")
}

func (ms *MemoryStore) hasLegacyDailyNotes() bool {
	entries, err := os.ReadDir(ms.memoryDir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if name == "MEMORY.md" || !strings.HasSuffix(name, ".md") {
			continue
		}

		stem := strings.TrimSuffix(name, ".md")
		if _, err := time.Parse("2006-01-02", stem); err == nil {
			return true
		}
	}

	return false
}

func (ms *MemoryStore) getTodayFileForAppend() string {
	legacy := ms.getTodayFileLegacy()
	nested := ms.getTodayFile()

	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	if _, err := os.Stat(nested); err == nil {
		return nested
	}

	if ms.hasLegacyDailyNotes() {
		return legacy
	}

	return nested
}

// ReadLongTerm reads the long-term memory (MEMORY.md).
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadLongTerm() string {
	if data, err := os.ReadFile(ms.memoryFile); err == nil {
		return string(data)
	}
	return ""
}

// WriteLongTerm writes content to the long-term memory file (MEMORY.md).
func (ms *MemoryStore) WriteLongTerm(content string) error {
	return os.WriteFile(ms.memoryFile, []byte(content), 0600)
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist.
func (ms *MemoryStore) ReadToday() string {
	for _, candidate := range []string{ms.getTodayFileLegacy(), ms.getTodayFile()} {
		if data, err := os.ReadFile(candidate); err == nil {
			return string(data)
		}
	}
	return ""
}

// AppendToday appends content to today's daily note.
// If the file doesn't exist, it creates a new file with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	todayFile := ms.getTodayFileForAppend()

	if err := os.MkdirAll(filepath.Dir(todayFile), 0700); err != nil {
		return fmt.Errorf("create memory directory: %w", err)
	}

	var existingContent string
	if data, err := os.ReadFile(todayFile); err == nil {
		existingContent = string(data)
	}

	var newContent string
	if existingContent == "" {
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		newContent = header + content
	} else {
		newContent = existingContent + "\n" + content
	}

	return os.WriteFile(todayFile, []byte(newContent), 0600)
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	var notes []string

	for i := 0; i < days; i++ {
		date := time.Now().AddDate(0, 0, -i)
		for _, candidate := range []string{ms.getDailyFileLegacy(date), ms.getDailyFileNested(date)} {
			if data, err := os.ReadFile(candidate); err == nil {
				notes = append(notes, string(data))
				break
			}
		}
	}

	if len(notes) == 0 {
		return ""
	}

	var result string
	for i, note := range notes {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += note
	}
	return result
}

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory and recent daily notes.
func (ms *MemoryStore) GetMemoryContext() string {
	var parts []string

	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		parts = append(parts, "## Long-term Memory\n\n"+longTerm)
	}

	recentNotes := ms.GetRecentDailyNotes(3)
	if recentNotes != "" {
		parts = append(parts, "## Recent Daily Notes\n\n"+recentNotes)
	}

	if len(parts) == 0 {
		return ""
	}

	var result string
	for i, part := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += part
	}
	return fmt.Sprintf("# Memory\n\n%s", result)
}

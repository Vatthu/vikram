package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/v1claw/levik/pkg/config"
	"github.com/v1claw/levik/pkg/epistemology"
	"github.com/v1claw/levik/pkg/logger"
	"github.com/v1claw/levik/pkg/providers"
	"github.com/v1claw/levik/pkg/skills"
	"github.com/v1claw/levik/pkg/tools"
)

// systemPromptCacheTTL is how long the built system prompt is considered fresh.
// During rapid tool loops (multiple LLM iterations per user message) the workspace
// files don't change, so re-reading them on every iteration is pure waste.
const systemPromptCacheTTL = 2 * time.Second

type ContextBuilder struct {
	workspace    string
	skillsLoader *skills.SkillsLoader
	memory       *MemoryStore
	tools        *tools.ToolRegistry // Direct reference to tool registry
	graphStore   epistemology.GraphStore
	teamRoles    string // injected team role descriptions for the system prompt

	// System-prompt cache: avoids redundant disk reads across tool loop iterations.
	promptCacheMu      sync.RWMutex
	promptCacheValue   string
	promptCacheBuiltAt time.Time
}

// SetTeamRoles injects team role descriptions into the system prompt so the
// main agent knows which specialist agents are available for delegation.
func (cb *ContextBuilder) SetTeamRoles(roles string) {
	cb.teamRoles = roles
}

func getGlobalConfigDir() string {
	return config.HomeDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func resolveBuiltinSkillsDir(workspace string) string {
	candidates := []string{
		filepath.Join(workspace, "skills"),
		config.GlobalSkillsDir(),
	}

	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "workspace", "skills"),
			filepath.Join(wd, "cmd", "levik", "workspace", "skills"),
			filepath.Join(wd, "skills"),
		)
	}

	for _, candidate := range candidates {
		if dirExists(candidate) {
			return candidate
		}
	}
	return ""
}

func NewContextBuilder(workspace string) *ContextBuilder {
	builtinSkillsDir := resolveBuiltinSkillsDir(workspace)
	globalSkillsDir := config.GlobalSkillsDir()

	return &ContextBuilder{
		workspace:    workspace,
		skillsLoader: skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:       NewMemoryStore(workspace),
	}
}

// SetToolsRegistry sets the tools registry for dynamic tool summary generation.
func (cb *ContextBuilder) SetToolsRegistry(registry *tools.ToolRegistry) {
	cb.tools = registry
}

// SetGraphStore wires the epistemology knowledge graph so learned facts
// are included in every system prompt as persistent memory.
func (cb *ContextBuilder) SetGraphStore(gs epistemology.GraphStore) {
	cb.graphStore = gs
}

func (cb *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	runtime := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())
	bootstrapPending := cb.hasPendingBootstrap()

	// Build tools section dynamically
	toolsSection := cb.buildToolsSection()

	prompt := fmt.Sprintf(`# Runtime Context

You are the assistant configured for this workspace.
Your identity, personality, relationship to the user, and operating style are defined by the workspace bootstrap files loaded below.
Treat those workspace files as authoritative over any generic defaults.

## Current Time
%s

## Runtime
%s

## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Daily Notes: %s/memory/YYYY-MM-DD.md or %s/memory/YYYYMM/YYYYMMDD.md
- Skills: %s/skills/{skill-name}/SKILL.md

%s

## Important Rules

1. **ALWAYS use tools** - When you need to perform an action (schedule reminders, send messages, execute commands, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it.

2. **Be helpful and accurate** - When using tools, briefly explain what you're doing.

3. **Memory** - When remembering something, write to %s/memory/MEMORY.md`,
		now, runtime, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath, toolsSection, workspacePath)

	if bootstrapPending {
		prompt += "\n\n## First-Run Bootstrap\n\n" +
			"BOOTSTRAP.md is present, which means this assistant is not fully formed yet.\n\n" +
			"- Do not pretend you already know your final name, persona, relationship, or preferences.\n" +
			"- If the operator asks about who you are, answer honestly that you are in bootstrap mode and need a few answers first.\n" +
			"- In the first private conversation, ask short discovery questions about:\n" +
			"  - who you should be to the operator\n" +
			"  - what you should call yourself\n" +
			"  - what you should call the operator\n" +
			"  - tone, boundaries, and priorities\n" +
			"- Once you have enough information, call the `complete_bootstrap` tool exactly once with the resolved identity details.\n" +
			"- Do not keep asking the same bootstrap questions after `complete_bootstrap` succeeds.\n"
	}

	return prompt
}

func (cb *ContextBuilder) buildToolsSection() string {
	if cb.tools == nil {
		return ""
	}

	summaries := cb.tools.GetSummaries()
	if len(summaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("**CRITICAL**: You MUST use tools to perform actions. Do NOT pretend to execute commands or schedule tasks.\n\n")

	if cb.teamRoles != "" {
		sb.WriteString("### Your Team\n\n")
		sb.WriteString("You lead a team of specialist agents. Delegate work to them using the `spawn`, `subagent`, or `delegate_task` tools with the appropriate worker type:\n\n")
		sb.WriteString(cb.teamRoles)
		sb.WriteString("\n")
	}

	sb.WriteString("### Your Tools\n\n")
	for _, s := range summaries {
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (cb *ContextBuilder) BuildSystemPrompt() string {
	// Fast path: return cached prompt if it is still fresh.
	cb.promptCacheMu.RLock()
	if time.Since(cb.promptCacheBuiltAt) < systemPromptCacheTTL && cb.promptCacheValue != "" {
		cached := cb.promptCacheValue
		cb.promptCacheMu.RUnlock()
		return cached
	}
	cb.promptCacheMu.RUnlock()

	// Slow path: rebuild from disk and update cache.
	cb.promptCacheMu.Lock()
	defer cb.promptCacheMu.Unlock()
	// Double-check after acquiring write lock (another goroutine may have rebuilt).
	if time.Since(cb.promptCacheBuiltAt) < systemPromptCacheTTL && cb.promptCacheValue != "" {
		return cb.promptCacheValue
	}

	prompt := cb.buildSystemPrompt()
	cb.promptCacheValue = prompt
	cb.promptCacheBuiltAt = time.Now()
	return prompt
}

// buildSystemPrompt is the actual (uncached) implementation.
func (cb *ContextBuilder) buildSystemPrompt() string {
	parts := []string{}

	// Core identity section
	parts = append(parts, cb.getIdentity())

	// Bootstrap files
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}

	// Skills - show summary, AI can read full content with read_file tool
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary))
	}

	// Memory context
	memoryContext := cb.memory.GetMemoryContext()
	if memoryContext != "" {
		parts = append(parts, "# Memory\n\n<user_memory>\n"+memoryContext+"\n</user_memory>")
	}

	// Epistemology: inject top high-confidence learned facts into the prompt
	// so the agent remembers what it has learned across sessions.
	if cb.graphStore != nil {
		facts, err := cb.graphStore.Query(epistemology.Query{MinConf: 0.6})
		if err != nil {
			logger.WarnCF("agent", "Failed to query epistemology for system prompt", map[string]interface{}{"error": err.Error()})
		} else if len(facts) > 0 {
			// Cap at 20 facts to avoid bloating the prompt.
			if len(facts) > 20 {
				facts = facts[:20]
			}
			var sb strings.Builder
			sb.WriteString("# Persistent Memory (Learned Facts)\n\n")
			sb.WriteString("The following facts were learned from prior interactions:\n\n")
			for _, f := range facts {
				sb.WriteString(fmt.Sprintf("- **%s** %s **%s**", f.Subject, f.Predicate, f.Object))
				if f.Source != "" {
					sb.WriteString(fmt.Sprintf(" _(source: %s)_", f.Source))
				}
				sb.WriteString("\n")
			}
			parts = append(parts, sb.String())
		}
	}

	// Join with "---" separator
	return strings.Join(parts, "\n\n---\n\n")
}

// InvalidatePromptCache forces the next BuildSystemPrompt call to rebuild from disk.
// Call this after any tool modifies workspace files (MEMORY.md, SOUL.md, etc.)
func (cb *ContextBuilder) InvalidatePromptCache() {
	cb.promptCacheMu.Lock()
	cb.promptCacheBuiltAt = time.Time{}
	cb.promptCacheMu.Unlock()
}

func (cb *ContextBuilder) hasPendingBootstrap() bool {
	_, err := os.Stat(filepath.Join(cb.workspace, "BOOTSTRAP.md"))
	return err == nil
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	type bootstrapEntry struct {
		label      string
		candidates []string
		required   bool
	}

	bootstrapFiles := []bootstrapEntry{
		{label: "AGENT.md", candidates: []string{"AGENTS.md", "AGENT.md"}, required: true},
		{label: "BOOTSTRAP.md", candidates: []string{"BOOTSTRAP.md"}},
		{label: "SOUL.md", candidates: []string{"SOUL.md"}, required: true},
		{label: "USER.md", candidates: []string{"USER.md"}, required: true},
		{label: "IDENTITY.md", candidates: []string{"IDENTITY.md"}, required: true},
		{label: "TOOLS.md", candidates: []string{"TOOLS.md"}},
		{label: "MEMORY.md", candidates: []string{"MEMORY.md"}},
		{label: "Legacy PROJECTS", candidates: []string{"core/memory/PROJECTS.md"}},
		{label: "Legacy DECISIONS", candidates: []string{"core/memory/DECISIONS.md"}},
		{label: "Legacy LESSONS", candidates: []string{"core/memory/LESSONS.md"}},
		{label: "Legacy PEOPLE", candidates: []string{"core/memory/PEOPLE.md"}},
		{label: "Legacy GRAND_STRATEGY", candidates: []string{"core/memory/GRAND_STRATEGY.md"}},
	}

	var result strings.Builder
	for _, entry := range bootstrapFiles {
		found := false
		for _, filename := range entry.candidates {
			filePath := filepath.Join(cb.workspace, filename)
			data, err := os.ReadFile(filePath)
			if err != nil {
				continue
			}
			result.WriteString(fmt.Sprintf("## %s\n\n<user_provided_content filename=%q>\n%s\n</user_provided_content>\n\n", entry.label, filename, string(data)))
			found = true
			break
		}
		if !found && entry.required {
			result.WriteString(fmt.Sprintf("## %s\n\n<missing_workspace_file filename=%q />\n\n", entry.label, entry.label))
		}
	}

	return result.String()
}

func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID string) []providers.Message {
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPrompt()

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}

	// Log system prompt summary for debugging (debug mode only)
	logger.DebugCF("agent", "System prompt built",
		map[string]interface{}{
			"total_chars":   len(systemPrompt),
			"total_lines":   strings.Count(systemPrompt, "\n") + 1,
			"section_count": strings.Count(systemPrompt, "\n\n---\n\n") + 1,
		})

	// Log preview of system prompt (avoid logging huge content)
	preview := systemPrompt
	if len(preview) > 500 {
		preview = preview[:500] + "... (truncated)"
	}
	logger.DebugCF("agent", "System prompt preview",
		map[string]interface{}{
			"preview": preview,
		})

	if summary != "" {
		systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
	}

	// Remove orphaned tool messages from history start to prevent LLM errors
	// (tool responses without preceding assistant tool_use are invalid).
	for len(history) > 0 && (history[0].Role == "tool") {
		logger.DebugCF("agent", "Removing orphaned tool message from history to prevent LLM error",
			map[string]interface{}{"role": history[0].Role})
		history = history[1:]
	}

	// Compress older observations to keep the context window manageable.
	history = compressHistory(history)

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	messages = append(messages, history...)

	if currentMessage != "" {
		messages = append(messages, providers.Message{
			Role:    "user",
			Content: currentMessage,
		})
	}

	return messages
}

func (cb *ContextBuilder) AddToolResult(messages []providers.Message, toolCallID, toolName, result string) []providers.Message {
	messages = append(messages, providers.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
	})
	return messages
}

func (cb *ContextBuilder) AddAssistantMessage(messages []providers.Message, content string, toolCalls []map[string]interface{}) []providers.Message {
	msg := providers.Message{
		Role:    "assistant",
		Content: content,
	}
	// Always add assistant message, whether or not it has tool calls
	messages = append(messages, msg)
	return messages
}

func (cb *ContextBuilder) loadSkills() string {
	allSkills := cb.skillsLoader.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var skillNames []string
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}

	content := cb.skillsLoader.LoadSkillsForContext(skillNames)
	if content == "" {
		return ""
	}

	return "# Skill Definitions\n\n" + content
}

// GetSkillsInfo returns information about loaded skills.
func (cb *ContextBuilder) GetSkillsInfo() map[string]interface{} {
	allSkills := cb.skillsLoader.ListSkills()
	skillNames := make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}
	return map[string]interface{}{
		"total":     len(allSkills),
		"available": len(allSkills),
		"names":     skillNames,
	}
}

const keepRecentObservations = 5

// compressHistory applies SWE-agent-style history processors to keep the
// context window manageable.  Recent observations are kept intact; older
// tool results and file views are collapsed to short summaries.
func compressHistory(history []providers.Message) []providers.Message {
	if len(history) <= keepRecentObservations+2 {
		return history
	}

	keepFrom := len(history) - keepRecentObservations
	compressed := make([]providers.Message, 0, len(history))

	for i, msg := range history {
		if i >= keepFrom {
			compressed = append(compressed, msg)
			continue
		}
		// Collapse old tool results to a one-line count.
		if msg.Role == "tool" || msg.Role == "user" {
			if strings.HasPrefix(msg.Content, "Observation:") {
				compressed = append(compressed, providers.Message{
					Role:    "tool",
					Content: "[earlier observation elided — " + strconv.Itoa(len(msg.Content)) + " chars]",
				})
				continue
			}
			if strings.Contains(msg.Content, "File:") && len(msg.Content) > 500 {
				compressed = append(compressed, providers.Message{
					Role:    "user",
					Content: "[earlier file view elided — " + strconv.Itoa(len(msg.Content)) + " chars]",
				})
				continue
			}
		}
		compressed = append(compressed, msg)
	}
	return compressed
}

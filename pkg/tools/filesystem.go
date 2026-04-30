package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/v1claw/levik/pkg/bus"
)

// validatePath ensures the given path is within the workspace if restrict is true.
func validatePath(path, workspace string, restrict bool) (string, error) {
	if workspace == "" {
		return path, nil
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	var absPath string
	if filepath.IsAbs(path) {
		absPath = filepath.Clean(path)
	} else {
		absPath, err = filepath.Abs(filepath.Join(absWorkspace, path))
		if err != nil {
			return "", fmt.Errorf("failed to resolve file path: %w", err)
		}
	}

	if restrict {
		// Resolve the real path of the workspace to prevent symlink traversal
		workspaceReal := absWorkspace
		if resolved, err := filepath.EvalSymlinks(absWorkspace); err == nil {
			workspaceReal = resolved
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("failed to resolve workspace symlink: %w", err)
		}

		// Resolve the real path of the given path. For non-existent paths (e.g. a
		// file about to be created), EvalSymlinks fails with IsNotExist. In that
		// case we walk up the ancestor chain until we find an existing directory and
		// resolve from there, so that platform symlinks (macOS /var→/private/var)
		// don't cause false "outside workspace" rejections for new nested paths.
		pathReal := absPath
		if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
			pathReal = resolved
		} else if os.IsNotExist(err) {
			// Walk up ancestors until we find one that can be resolved.
			absDir := filepath.Dir(absPath)
			suffix := filepath.Base(absPath)
			for absDir != filepath.Dir(absDir) { // stop at filesystem root
				if resolved, rerr := filepath.EvalSymlinks(absDir); rerr == nil {
					pathReal = filepath.Join(resolved, suffix)
					break
				} else if !os.IsNotExist(rerr) {
					break // unexpected error, leave pathReal as absPath
				}
				suffix = filepath.Join(filepath.Base(absDir), suffix)
				absDir = filepath.Dir(absDir)
			}
		} else {
			return "", fmt.Errorf("failed to resolve path symlink: %w", err)
		}

		if !isWithinWorkspace(pathReal, workspaceReal) {
			return "", fmt.Errorf("access denied: path is outside the workspace or resolves outside via symlink")
		}
	}

	return absPath, nil
}

func isWithinWorkspace(candidate, workspace string) bool {
	rel, err := filepath.Rel(filepath.Clean(workspace), filepath.Clean(candidate))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

type ReadFileTool struct {
	workspace string
	restrict  bool
	bus       *bus.MessageBus
}

func NewReadFileTool(workspace string, restrict bool, msgBus *bus.MessageBus) *ReadFileTool {
	return &ReadFileTool{workspace: workspace, restrict: restrict, bus: msgBus}
}

func (t *ReadFileTool) Name() string {
	return "read_file"
}

func (t *ReadFileTool) Description() string {
	return "Read the contents of a file"
}

func (t *ReadFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to read",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ReadFileTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	// [CRITICAL] Filesystem tools have TOCTOU window and unbounded reads.
	// Fix: Add a max_bytes limit to prevent OOM.
	const maxReadBytes = 10 * 1024 * 1024 // 10MB limit for reads

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		if t.bus != nil && tc.Channel != "" {
			t.bus.PublishOutbound(bus.OutboundMessage{
				Channel: tc.Channel,
				ChatID:  tc.ChatID,
				Content: fmt.Sprintf("⚠️ **Security Alert**: I attempted to read `%s` but was blocked by your Strict Sandbox configuration. I am restricted to `%s`.", path, t.workspace),
			})
		}
		return ErrorResult(err.Error())
	}

	// Open file first to tie the check to the actual file descriptor
	f, err := os.Open(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to open file: %v", err))
	}
	defer f.Close()

	if t.restrict {
		// TOCTOU mitigation: verify the opened file matches the resolved path's Lstat
		// This prevents swapping the file with a symlink after validatePath
		fi, err := f.Stat()
		if err == nil {
			li, lerr := os.Lstat(resolvedPath)
			if lerr == nil && !os.SameFile(fi, li) {
				return ErrorResult("access denied: symlink race detected (TOCTOU)")
			}
		}
	}

	info, err := f.Stat()
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to stat file: %v", err))
	}
	if info.Size() > maxReadBytes {
		return ErrorResult(fmt.Sprintf("file too large to read (max %d bytes)", maxReadBytes))
	}

	content, err := io.ReadAll(f)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	return NewToolResult(string(content))
}

type WriteFileTool struct {
	workspace string
	restrict  bool
	bus       *bus.MessageBus
}

func NewWriteFileTool(workspace string, restrict bool, msgBus *bus.MessageBus) *WriteFileTool {
	return &WriteFileTool{workspace: workspace, restrict: restrict, bus: msgBus}
}

func (t *WriteFileTool) Name() string {
	return "write_file"
}

func (t *WriteFileTool) Description() string {
	return "Write content to a file"
}

func (t *WriteFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to the file to write",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Content to write to the file",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *WriteFileTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("content is required")
	}

	const maxWriteBytes = 50 * 1024 * 1024 // 50 MB
	if len(content) > maxWriteBytes {
		return ErrorResult(fmt.Sprintf("content too large to write (max %d bytes)", maxWriteBytes))
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		if t.bus != nil && tc.Channel != "" {
			t.bus.PublishOutbound(bus.OutboundMessage{
				Channel: tc.Channel,
				ChatID:  tc.ChatID,
				Content: fmt.Sprintf("⚠️ **Security Alert**: I attempted to write to `%s` but was blocked by your Strict Sandbox configuration. I am restricted to `%s`.", path, t.workspace),
			})
		}
		return ErrorResult(err.Error())
	}

	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create directory: %v", err))
	}

	// V1: Symlink check BEFORE open — prevents TOCTOU where O_TRUNC would
	// destroy the target before the check runs.
	if t.restrict {
		if li, lerr := os.Lstat(resolvedPath); lerr == nil && (li.Mode()&os.ModeSymlink != 0) {
			return ErrorResult("access denied: path resolves to a symlink")
		}
	}

	f, err := os.OpenFile(resolvedPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to open file for writing: %v", err))
	}
	defer f.Close()

	if _, err := f.Write([]byte(content)); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	return SilentResult(fmt.Sprintf("File written: %s", path))
}

type ListDirTool struct {
	workspace string
	restrict  bool
	bus       *bus.MessageBus
}

func NewListDirTool(workspace string, restrict bool, msgBus *bus.MessageBus) *ListDirTool {
	return &ListDirTool{workspace: workspace, restrict: restrict, bus: msgBus}
}

func (t *ListDirTool) Name() string {
	return "list_dir"
}

func (t *ListDirTool) Description() string {
	return "List files and directories in a path"
}

func (t *ListDirTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Path to list",
			},
		},
		"required": []string{"path"},
	}
}

func (t *ListDirTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		path = "."
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		if t.bus != nil && tc.Channel != "" {
			t.bus.PublishOutbound(bus.OutboundMessage{
				Channel: tc.Channel,
				ChatID:  tc.ChatID,
				Content: fmt.Sprintf("⚠️ **Security Alert**: I attempted to browse `%s` but was blocked by your Strict Sandbox configuration. I am restricted to `%s`.", path, t.workspace),
			})
		}
		return ErrorResult(err.Error())
	}

	entries, err := os.ReadDir(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read directory: %v", err))
	}

	result := ""
	for _, entry := range entries {
		if entry.IsDir() {
			result += "DIR:  " + entry.Name() + "\n"
		} else {
			result += "FILE: " + entry.Name() + "\n"
		}
	}

	return NewToolResult(result)
}

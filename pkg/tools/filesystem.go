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

type safeLocalPath struct {
	root string
	rel  string
}

// validatePath converts an agent-provided path into a workspace-relative path.
// Filesystem tools intentionally reject absolute paths: the host owns the root,
// and models only choose a local path within that root.
func validatePath(path, workspace string, restrict bool) (safeLocalPath, error) {
	path = strings.TrimSpace(path)
	workspace = strings.TrimSpace(workspace)
	if path == "" {
		return safeLocalPath{}, fmt.Errorf("path is required")
	}
	if strings.Contains(path, "\x00") || strings.Contains(workspace, "\x00") {
		return safeLocalPath{}, fmt.Errorf("path contains unsupported characters")
	}
	if workspace == "" {
		if restrict {
			return safeLocalPath{}, fmt.Errorf("workspace is required when filesystem restriction is enabled")
		}
		workspace = "."
	}
	if filepath.IsAbs(path) || !filepath.IsLocal(path) {
		return safeLocalPath{}, fmt.Errorf("access denied: path is outside the workspace")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return safeLocalPath{}, fmt.Errorf("failed to resolve workspace path: %w", err)
	}

	workspaceReal, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return safeLocalPath{}, fmt.Errorf("failed to resolve workspace symlink: %w", err)
	}

	rel := filepath.Clean(path)
	if rel == "." {
		return safeLocalPath{root: workspaceReal, rel: rel}, nil
	}

	absPath, err := filepath.Abs(filepath.Join(workspaceReal, rel))
	if err != nil {
		return safeLocalPath{}, fmt.Errorf("failed to resolve file path: %w", err)
	}
	pathReal, err := resolvePathForContainment(absPath)
	if err != nil {
		return safeLocalPath{}, fmt.Errorf("failed to resolve path symlink: %w", err)
	}
	if !isWithinWorkspace(pathReal, workspaceReal) {
		return safeLocalPath{}, fmt.Errorf("access denied: path is outside the workspace or resolves outside via symlink")
	}
	return safeLocalPath{root: workspaceReal, rel: rel}, nil
}

func resolvePathForContainment(absPath string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		return resolved, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}

	absDir := filepath.Dir(absPath)
	suffix := filepath.Base(absPath)
	for {
		if resolved, err := filepath.EvalSymlinks(absDir); err == nil {
			return filepath.Join(resolved, suffix), nil
		} else if !os.IsNotExist(err) {
			return "", err
		}

		parent := filepath.Dir(absDir)
		if parent == absDir {
			return "", os.ErrNotExist
		}
		suffix = filepath.Join(filepath.Base(absDir), suffix)
		absDir = parent
	}
}

func isWithinWorkspace(candidate, workspace string) bool {
	rel, err := filepath.Rel(filepath.Clean(workspace), filepath.Clean(candidate))
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))))
}

func openRegularFileForRead(path safeLocalPath) (*os.File, os.FileInfo, error) {
	rel := path.rel
	if !filepath.IsLocal(rel) {
		return nil, nil, fmt.Errorf("access denied: path is outside the workspace")
	}
	fullPath := filepath.Join(path.root, rel)
	f, err := os.Open(fullPath)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, nil, fmt.Errorf("path must be a file")
	}
	linkInfo, err := os.Lstat(fullPath)
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if linkInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, linkInfo) {
		_ = f.Close()
		return nil, nil, fmt.Errorf("access denied: symlink race detected")
	}
	return f, info, nil
}

func readDirectoryEntries(path safeLocalPath) ([]os.DirEntry, error) {
	rel := path.rel
	if !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("access denied: path is outside the workspace")
	}
	fullPath := filepath.Join(path.root, rel)
	info, err := os.Lstat(fullPath)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("access denied: path resolves to a symlink")
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path must be a directory")
	}
	return os.ReadDir(fullPath)
}

func writeFileReplacingPath(path safeLocalPath, content []byte, defaultMode os.FileMode) error {
	rel := path.rel
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("access denied: path is outside the workspace")
	}
	fullPath := filepath.Join(path.root, rel)
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	mode := defaultMode
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("access denied: path resolves to a symlink")
		}
		if info.IsDir() {
			return fmt.Errorf("path must be a file")
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".levik-write-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, fullPath); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func appendFileReplacingPath(path safeLocalPath, content []byte, maxExistingBytes int64) error {
	rel := path.rel
	if !filepath.IsLocal(rel) {
		return fmt.Errorf("access denied: path is outside the workspace")
	}
	fullPath := filepath.Join(path.root, rel)
	mode := os.FileMode(0o600)
	var existing []byte
	if info, err := os.Lstat(fullPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("access denied: path resolves to a symlink")
		}
		if info.IsDir() {
			return fmt.Errorf("path must be a file")
		}
		if maxExistingBytes > 0 && info.Size() > maxExistingBytes {
			return fmt.Errorf("file too large to append safely (max %d bytes)", maxExistingBytes)
		}
		f, _, err := openRegularFileForRead(path)
		if err != nil {
			return err
		}
		existing, err = io.ReadAll(f)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	combined := make([]byte, 0, len(existing)+len(content))
	combined = append(combined, existing...)
	combined = append(combined, content...)
	return writeFileReplacingPath(path, combined, mode)
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

	f, info, err := openRegularFileForRead(resolvedPath)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to open file: %v", err))
	}
	defer f.Close()

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

	if err := writeFileReplacingPath(resolvedPath, []byte(content), 0o600); err != nil {
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

	entries, err := readDirectoryEntries(resolvedPath)
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

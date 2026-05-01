package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

// EditFileTool edits a file by replacing old_text with new_text.
// The old_text must exist exactly in the file.
type EditFileTool struct {
	allowedDir string
	restrict   bool
}

// NewEditFileTool creates a new EditFileTool with optional directory restriction.
func NewEditFileTool(allowedDir string, restrict bool) *EditFileTool {
	return &EditFileTool{
		allowedDir: allowedDir,
		restrict:   restrict,
	}
}

func (t *EditFileTool) Name() string {
	return "edit_file"
}

func (t *EditFileTool) Description() string {
	return "Edit a file by replacing old_text with new_text. The old_text must exist exactly in the file."
}

func (t *EditFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path to edit",
			},
			"old_text": map[string]interface{}{
				"type":        "string",
				"description": "The exact text to find and replace",
			},
			"new_text": map[string]interface{}{
				"type":        "string",
				"description": "The text to replace with",
			},
		},
		"required": []string{"path", "old_text", "new_text"},
	}
}

func (t *EditFileTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) *ToolResult {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	oldText, ok := args["old_text"].(string)
	if !ok {
		return ErrorResult("old_text is required")
	}

	newText, ok := args["new_text"].(string)
	if !ok {
		return ErrorResult("new_text is required")
	}

	resolvedPath, err := validatePath(path, t.allowedDir, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	const maxEditBytes = 10 * 1024 * 1024
	f, fi, err := openRegularFileForRead(resolvedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return ErrorResult(fmt.Sprintf("file not found: %s", path))
		}
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}
	defer f.Close()
	if fi.Size() > maxEditBytes {
		return ErrorResult(fmt.Sprintf("file too large to edit (max %d bytes)", maxEditBytes))
	}
	content, err := io.ReadAll(f)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
	}

	contentStr := string(content)

	if !strings.Contains(contentStr, oldText) {
		return ErrorResult("The specified text was not found in the file. Make sure it matches exactly.")
	}

	count := strings.Count(contentStr, oldText)
	if count > 1 {
		return ErrorResult(fmt.Sprintf("The specified text appears %d times in the file. Please provide more context to make it unique.", count))
	}

	newContent := strings.Replace(contentStr, oldText, newText, 1)

	// Preserve the original file's permissions.
	mode := fi.Mode().Perm()

	if err := writeFileReplacingPath(resolvedPath, []byte(newContent), mode); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write file: %v", err))
	}

	return SilentResult(fmt.Sprintf("File edited: %s", path))
}

type AppendFileTool struct {
	workspace string
	restrict  bool
}

func NewAppendFileTool(workspace string, restrict bool) *AppendFileTool {
	return &AppendFileTool{workspace: workspace, restrict: restrict}
}

func (t *AppendFileTool) Name() string {
	return "append_file"
}

func (t *AppendFileTool) Description() string {
	return "Append content to the end of a file"
}

func (t *AppendFileTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path to append to",
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The content to append",
			},
		},
		"required": []string{"path", "content"},
	}
}

func (t *AppendFileTool) Execute(ctx context.Context, tc ToolContext, args map[string]interface{}) (res *ToolResult) {
	path, ok := args["path"].(string)
	if !ok {
		return ErrorResult("path is required")
	}

	content, ok := args["content"].(string)
	if !ok {
		return ErrorResult("content is required")
	}

	resolvedPath, err := validatePath(path, t.workspace, t.restrict)
	if err != nil {
		return ErrorResult(err.Error())
	}

	const maxAppendBytes = 50 * 1024 * 1024
	if len(content) > maxAppendBytes {
		return ErrorResult(fmt.Sprintf("content too large to append (max %d bytes)", maxAppendBytes))
	}
	if err := appendFileReplacingPath(resolvedPath, []byte(content), maxAppendBytes); err != nil {
		return ErrorResult(fmt.Sprintf("failed to append to file: %v", err))
	}

	res = SilentResult(fmt.Sprintf("Appended to %s", path))
	return
}

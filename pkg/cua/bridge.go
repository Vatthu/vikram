// Package cua provides the CUA Driver bridge for native macOS desktop automation.
// It spawns cua-driver as an MCP subprocess and exposes consolidated computer-use
// tools that integrate with Vikram's tool and permission systems.
package cua

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/logger"
	"github.com/Vatthu/vikram/pkg/mcp"
	"github.com/Vatthu/vikram/pkg/permissions"
	"github.com/Vatthu/vikram/pkg/tools"
)

// Bridge manages the lifecycle of a cua-driver MCP subprocess and
// exposes consolidated computer-use tools for the Vikram agent.
type Bridge struct {
	client *mcp.Client
	mu     sync.Mutex
	config config.CUAConfig
}

// NewBridge spawns cua-driver in MCP mode and discovers its tools.
// Returns an error if the driver binary is not found or fails to initialize.
func NewBridge(ctx context.Context, cfg config.CUAConfig) (*Bridge, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("CUA Driver is only supported on macOS (current OS: %s)", runtime.GOOS)
	}

	driverPath := cfg.DriverPath
	if driverPath == "" {
		driverPath = "cua-driver"
	}

	// Verify the binary exists before attempting to spawn.
	if _, err := exec.LookPath(driverPath); err != nil {
		return nil, fmt.Errorf("cua-driver binary not found in PATH: %w (install: https://github.com/trycua/cua)", err)
	}

	timeout := time.Duration(cfg.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	client, err := mcp.NewClient(ctx, mcp.ClientConfig{
		Command: driverPath,
		Args:    []string{"mcp"},
		Timeout: timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start cua-driver MCP server: %w", err)
	}

	logger.InfoCF("cua", "CUA Driver started", map[string]interface{}{
		"driver":     driverPath,
		"tools":      len(client.Tools()),
		"timeout_ms": timeout.Milliseconds(),
	})

	return &Bridge{
		client: client,
		config: cfg,
	}, nil
}

// Close terminates the cua-driver subprocess.
func (b *Bridge) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client != nil {
		logger.InfoC("cua", "Shutting down CUA Driver")
		return b.client.Close()
	}
	return nil
}

// Tools returns the consolidated set of Vikram tools backed by CUA Driver.
func (b *Bridge) Tools() []tools.Tool {
	return []tools.Tool{
		&cuaTool{bridge: b, def: cuaToolDef{
			name: "computer_screenshot",
			desc: "Capture the current state of a macOS application window including its accessibility tree " +
				"(UI elements, labels, positions) and optionally a screenshot. Use list mode first to find " +
				"available apps and windows, then target a specific window.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"list_apps", "list_windows", "get_state"},
						"description": "Action: 'list_apps' to list running apps, 'list_windows' to list windows for a PID, 'get_state' to capture window state with accessibility tree",
					},
					"pid": map[string]interface{}{
						"type":        "integer",
						"description": "Process ID of the target application (required for list_windows and get_state)",
					},
					"window_id": map[string]interface{}{
						"type":        "integer",
						"description": "Window ID (required for get_state)",
					},
				},
				"required": []string{"action"},
			},
		}},
		&cuaTool{bridge: b, def: cuaToolDef{
			name: "computer_click",
			desc: "Click on a UI element in a macOS application window. Supports left click, right click, " +
				"and double click. Target elements by their index from the accessibility tree " +
				"(from computer_screenshot get_state) or by absolute pixel coordinates.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pid": map[string]interface{}{
						"type":        "integer",
						"description": "Process ID of the target application",
					},
					"window_id": map[string]interface{}{
						"type":        "integer",
						"description": "Window ID of the target window",
					},
					"element_index": map[string]interface{}{
						"type":        "integer",
						"description": "Element index from the accessibility tree (preferred over coordinates)",
					},
					"x": map[string]interface{}{
						"type":        "integer",
						"description": "Absolute X pixel coordinate (use only if element_index unavailable)",
					},
					"y": map[string]interface{}{
						"type":        "integer",
						"description": "Absolute Y pixel coordinate (use only if element_index unavailable)",
					},
					"click_type": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"left", "right", "double"},
						"description": "Click type (default: left)",
					},
				},
				"required": []string{"pid", "window_id"},
			},
		}},
		&cuaTool{bridge: b, def: cuaToolDef{
			name: "computer_type",
			desc: "Type text or press keyboard keys in a macOS application. Supports plain text input, " +
				"individual key presses (Return, Escape, Tab, arrow keys), and hotkey combos (Cmd+C, Cmd+V, etc.).",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"action": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"type_text", "press_key", "hotkey"},
						"description": "Action: 'type_text' for text input, 'press_key' for a single key, 'hotkey' for key combos",
					},
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to type (for type_text action)",
					},
					"key": map[string]interface{}{
						"type":        "string",
						"description": "Key name like 'Return', 'Escape', 'Tab', 'ArrowUp' (for press_key action)",
					},
					"keys": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Key combo like ['cmd', 'c'] for Cmd+C (for hotkey action)",
					},
					"pid": map[string]interface{}{
						"type":        "integer",
						"description": "Process ID of the target application (optional — targets focused app if omitted)",
					},
				},
				"required": []string{"action"},
			},
		}},
		&cuaTool{bridge: b, def: cuaToolDef{
			name: "computer_launch",
			desc: "Launch a macOS application by its bundle identifier (e.g., com.apple.calculator, " +
				"com.apple.Safari, com.apple.Terminal). The app is launched in the background " +
				"without stealing focus from the user's current workspace.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"bundle_id": map[string]interface{}{
						"type":        "string",
						"description": "macOS bundle identifier (e.g., 'com.apple.calculator')",
					},
				},
				"required": []string{"bundle_id"},
			},
		}},
		&cuaTool{bridge: b, def: cuaToolDef{
			name: "computer_scroll",
			desc: "Scroll within a macOS application window. Scroll direction can be up, down, left, or right.",
			params: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"pid": map[string]interface{}{
						"type":        "integer",
						"description": "Process ID of the target application",
					},
					"window_id": map[string]interface{}{
						"type":        "integer",
						"description": "Window ID of the target window",
					},
					"direction": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"up", "down", "left", "right"},
						"description": "Scroll direction",
					},
					"amount": map[string]interface{}{
						"type":        "integer",
						"description": "Scroll amount in pixels (default: 100)",
					},
				},
				"required": []string{"pid", "window_id", "direction"},
			},
		}},
	}
}

// cuaToolDef holds the static definition for a CUA tool.
type cuaToolDef struct {
	name   string
	desc   string
	params map[string]interface{}
}

// cuaTool implements the tools.Tool interface for CUA actions.
type cuaTool struct {
	bridge *Bridge
	def    cuaToolDef
}

func (t *cuaTool) Name() string                       { return t.def.name }
func (t *cuaTool) Description() string                { return t.def.desc }
func (t *cuaTool) Parameters() map[string]interface{} { return t.def.params }

func (t *cuaTool) Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult {
	// Permission gate: computer_use must be enabled.
	if err := permissions.Global().Check(permissions.ComputerUse, t.def.name); err != nil {
		return tools.ErrorResult(err.Error())
	}

	switch t.def.name {
	case "computer_screenshot":
		return t.executeScreenshot(ctx, args)
	case "computer_click":
		return t.executeClick(ctx, args)
	case "computer_type":
		return t.executeType(ctx, args)
	case "computer_launch":
		return t.executeLaunch(ctx, args)
	case "computer_scroll":
		return t.executeScroll(ctx, args)
	default:
		return tools.ErrorResult(fmt.Sprintf("unknown CUA tool: %s", t.def.name))
	}
}

func (t *cuaTool) executeScreenshot(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	action, _ := args["action"].(string)
	switch action {
	case "list_apps":
		return t.callCUA(ctx, "list_apps", map[string]interface{}{})
	case "list_windows":
		pid, ok := args["pid"]
		if !ok {
			return tools.ErrorResult("pid is required for list_windows")
		}
		return t.callCUA(ctx, "list_windows", map[string]interface{}{"pid": pid})
	case "get_state":
		pid, ok1 := args["pid"]
		wid, ok2 := args["window_id"]
		if !ok1 || !ok2 {
			return tools.ErrorResult("pid and window_id are required for get_state")
		}
		return t.callCUA(ctx, "get_window_state", map[string]interface{}{
			"pid":       pid,
			"window_id": wid,
		})
	default:
		return tools.ErrorResult(fmt.Sprintf("unknown screenshot action: %q (use list_apps, list_windows, or get_state)", action))
	}
}

func (t *cuaTool) executeClick(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	clickType, _ := args["click_type"].(string)
	if clickType == "" {
		clickType = "left"
	}

	cuaArgs := make(map[string]interface{})
	for _, key := range []string{"pid", "window_id", "element_index", "x", "y"} {
		if v, ok := args[key]; ok {
			cuaArgs[key] = v
		}
	}

	var toolName string
	switch clickType {
	case "right":
		toolName = "right_click"
	case "double":
		toolName = "double_click"
	default:
		toolName = "click"
	}

	return t.callCUA(ctx, toolName, cuaArgs)
}

func (t *cuaTool) executeType(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	action, _ := args["action"].(string)
	switch action {
	case "type_text":
		text, ok := args["text"].(string)
		if !ok || text == "" {
			return tools.ErrorResult("text is required for type_text action")
		}
		cuaArgs := map[string]interface{}{"text": text}
		if pid, ok := args["pid"]; ok {
			cuaArgs["pid"] = pid
		}
		return t.callCUA(ctx, "type_text", cuaArgs)

	case "press_key":
		key, ok := args["key"].(string)
		if !ok || key == "" {
			return tools.ErrorResult("key is required for press_key action")
		}
		cuaArgs := map[string]interface{}{"key": key}
		if pid, ok := args["pid"]; ok {
			cuaArgs["pid"] = pid
		}
		return t.callCUA(ctx, "press_key", cuaArgs)

	case "hotkey":
		keys, ok := args["keys"]
		if !ok {
			return tools.ErrorResult("keys array is required for hotkey action")
		}
		cuaArgs := map[string]interface{}{"keys": keys}
		if pid, ok := args["pid"]; ok {
			cuaArgs["pid"] = pid
		}
		return t.callCUA(ctx, "hotkey", cuaArgs)

	default:
		return tools.ErrorResult(fmt.Sprintf("unknown type action: %q (use type_text, press_key, or hotkey)", action))
	}
}

func (t *cuaTool) executeLaunch(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	bundleID, ok := args["bundle_id"].(string)
	if !ok || bundleID == "" {
		return tools.ErrorResult("bundle_id is required")
	}
	return t.callCUA(ctx, "launch_app", map[string]interface{}{"bundle_id": bundleID})
}

func (t *cuaTool) executeScroll(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	cuaArgs := make(map[string]interface{})
	for _, key := range []string{"pid", "window_id", "direction", "amount"} {
		if v, ok := args[key]; ok {
			cuaArgs[key] = v
		}
	}
	return t.callCUA(ctx, "scroll", cuaArgs)
}

// callCUA dispatches a tool call to the cua-driver MCP bridge and formats the result.
func (t *cuaTool) callCUA(ctx context.Context, toolName string, args map[string]interface{}) *tools.ToolResult {
	logger.DebugCF("cua", fmt.Sprintf("CUA call: %s", toolName), map[string]interface{}{
		"tool": toolName,
		"args": args,
	})

	t.bridge.mu.Lock()
	client := t.bridge.client
	t.bridge.mu.Unlock()

	if client == nil {
		return tools.ErrorResult("CUA Driver is not running")
	}

	result, err := client.CallTool(ctx, toolName, args)
	if err != nil {
		logger.WarnCF("cua", fmt.Sprintf("CUA tool %s failed", toolName), map[string]interface{}{
			"error": err.Error(),
			"tool":  toolName,
		})
		return tools.ErrorResult(fmt.Sprintf("CUA Driver error (%s): %v", toolName, err))
	}

	// Extract text content from the MCP result.
	var parts []string
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			parts = append(parts, block.Text)
		case "image":
			parts = append(parts, fmt.Sprintf("[screenshot: %s, %d bytes base64]", block.MimeType, len(block.Data)))
		case "resource":
			parts = append(parts, fmt.Sprintf("[resource: %s]", block.MimeType))
		}
	}

	output := ""
	if len(parts) > 0 {
		for i, p := range parts {
			if i > 0 {
				output += "\n"
			}
			output += p
		}
	} else {
		output = "OK (no output)"
	}

	// Truncate very large outputs (e.g., huge accessibility trees).
	const maxOutput = 80000
	if len(output) > maxOutput {
		output = output[:maxOutput] + fmt.Sprintf("\n\n[CUA output truncated: %d total chars]", len(output))
	}

	return &tools.ToolResult{
		ForLLM:  output,
		ForUser: formatCUAUserResult(toolName, args, result.IsError),
		IsError: result.IsError,
	}
}

// formatCUAUserResult builds a user-facing summary of a CUA action.
func formatCUAUserResult(toolName string, args map[string]interface{}, isError bool) string {
	if isError {
		return fmt.Sprintf("❌ CUA %s failed", toolName)
	}
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("🖥️ CUA %s %s", toolName, string(argsJSON))
}

// IsAvailable checks if cua-driver is installed and available in PATH.
func IsAvailable() (string, bool) {
	path, err := exec.LookPath("cua-driver")
	if err != nil {
		return "", false
	}
	return path, true
}

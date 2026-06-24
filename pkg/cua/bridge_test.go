package cua

import (
	"context"
	"runtime"
	"testing"

	"github.com/Vatthu/vikram/pkg/config"
	"github.com/Vatthu/vikram/pkg/permissions"
)

func TestToolsDefinitions(t *testing.T) {
	bridge := &Bridge{
		config: config.CUAConfig{
			Enabled:    true,
			DriverPath: "cua-driver",
			Timeout:    30,
		},
	}

	tools := bridge.Tools()

	expectedNames := map[string]bool{
		"computer_screenshot": true,
		"computer_click":      true,
		"computer_type":       true,
		"computer_launch":     true,
		"computer_scroll":     true,
	}

	if len(tools) != len(expectedNames) {
		t.Fatalf("expected %d tools, got %d", len(expectedNames), len(tools))
	}

	for _, tool := range tools {
		if !expectedNames[tool.Name()] {
			t.Errorf("unexpected tool: %s", tool.Name())
		}
		if tool.Description() == "" {
			t.Errorf("tool %s has empty description", tool.Name())
		}
		if tool.Parameters() == nil {
			t.Errorf("tool %s has nil parameters", tool.Name())
		}
	}
}

func TestPermissionGating(t *testing.T) {
	reg := permissions.NewRegistry()

	if reg.IsAllowed(permissions.ComputerUse) {
		t.Fatal("computer_use should be denied by default")
	}

	bridge := &Bridge{
		config: config.CUAConfig{Enabled: true},
	}

	tools := bridge.Tools()

	// Verify all tools are denied by default.
	for _, tool := range tools {
		ct := tool.(*cuaTool)
		err := reg.Check(permissions.ComputerUse, ct.Name())
		if err == nil {
			t.Errorf("tool %s should be denied when computer_use is not enabled", ct.Name())
		}
	}

	// Enable and verify.
	if err := reg.Set(permissions.ComputerUse, true); err != nil {
		t.Fatalf("failed to enable computer_use: %v", err)
	}

	for _, tool := range tools {
		ct := tool.(*cuaTool)
		err := reg.Check(permissions.ComputerUse, ct.Name())
		if err != nil {
			t.Errorf("tool %s should be allowed when computer_use is enabled: %v", ct.Name(), err)
		}
	}
}

func TestBridgeRequiresDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("skipping non-darwin test on darwin")
	}

	cfg := config.CUAConfig{
		Enabled:    true,
		DriverPath: "cua-driver",
		Timeout:    30,
	}

	_, err := NewBridge(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error on non-darwin OS")
	}
}

func TestAvailabilityCheck(t *testing.T) {
	path, available := IsAvailable()
	_ = path
	_ = available
}

func TestScreenshotToolValidation(t *testing.T) {
	bridge := &Bridge{
		config: config.CUAConfig{Enabled: true},
	}

	tools := bridge.Tools()
	var screenshotTool *cuaTool
	for _, tool := range tools {
		if tool.Name() == "computer_screenshot" {
			screenshotTool = tool.(*cuaTool)
			break
		}
	}
	if screenshotTool == nil {
		t.Fatal("computer_screenshot tool not found")
	}

	// Test get_state without pid.
	result := screenshotTool.executeScreenshot(context.Background(), map[string]interface{}{
		"action": "get_state",
	})
	if !result.IsError {
		t.Error("expected error when pid is missing for get_state")
	}

	// Test unknown action.
	result = screenshotTool.executeScreenshot(context.Background(), map[string]interface{}{
		"action": "unknown",
	})
	if !result.IsError {
		t.Error("expected error for unknown action")
	}
}

func TestTypeToolValidation(t *testing.T) {
	bridge := &Bridge{
		config: config.CUAConfig{Enabled: true},
	}

	tools := bridge.Tools()
	var typeTool *cuaTool
	for _, tool := range tools {
		if tool.Name() == "computer_type" {
			typeTool = tool.(*cuaTool)
			break
		}
	}
	if typeTool == nil {
		t.Fatal("computer_type tool not found")
	}

	tests := []struct {
		name string
		args map[string]interface{}
	}{
		{"type_text without text", map[string]interface{}{"action": "type_text"}},
		{"press_key without key", map[string]interface{}{"action": "press_key"}},
		{"hotkey without keys", map[string]interface{}{"action": "hotkey"}},
		{"unknown action", map[string]interface{}{"action": "unknown"}},
	}

	for _, tt := range tests {
		result := typeTool.executeType(context.Background(), tt.args)
		if !result.IsError {
			t.Errorf("%s: expected error", tt.name)
		}
	}
}

func TestLaunchToolValidation(t *testing.T) {
	bridge := &Bridge{
		config: config.CUAConfig{Enabled: true},
	}

	tools := bridge.Tools()
	var launchTool *cuaTool
	for _, tool := range tools {
		if tool.Name() == "computer_launch" {
			launchTool = tool.(*cuaTool)
			break
		}
	}
	if launchTool == nil {
		t.Fatal("computer_launch tool not found")
	}

	result := launchTool.executeLaunch(context.Background(), map[string]interface{}{})
	if !result.IsError {
		t.Error("expected error when bundle_id is missing")
	}
}

func TestFormatCUAUserResult(t *testing.T) {
	result := formatCUAUserResult("click", map[string]interface{}{"pid": 123}, true)
	if result != "❌ CUA click failed" {
		t.Errorf("unexpected error result: %s", result)
	}

	result = formatCUAUserResult("launch_app", map[string]interface{}{"bundle_id": "com.apple.calculator"}, false)
	if result == "" {
		t.Error("expected non-empty success result")
	}
}

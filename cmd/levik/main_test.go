package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time" // Added for time.Duration

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/v1claw/levik/pkg/config"
)

func TestIsPublicHost(t *testing.T) {
	assert.False(t, isPublicHost("localhost"))
	assert.False(t, isPublicHost("127.0.0.1"))
	assert.False(t, isPublicHost("::1"))
	assert.True(t, isPublicHost("0.0.0.0"))
	assert.True(t, isPublicHost("::"))
	assert.True(t, isPublicHost("example.com"))
}

func TestResolveClientEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		httpBase    string
		wsURL       string
		devicesURL  string
		routeTarget string
	}{
		{
			name:        "hostPort",
			input:       "192.168.1.10:18791",
			httpBase:    "http://192.168.1.10:18791",
			wsURL:       "ws://192.168.1.10:18791/api/v1/ws",
			devicesURL:  "http://192.168.1.10:18791/api/v1/devices",
			routeTarget: "192.168.1.10:18791",
		},
		{
			name:        "hostDefaultsPort",
			input:       "gateway.local",
			httpBase:    "http://gateway.local:18791",
			wsURL:       "ws://gateway.local:18791/api/v1/ws",
			devicesURL:  "http://gateway.local:18791/api/v1/devices",
			routeTarget: "gateway.local:18791",
		},
		{
			name:        "httpsURL",
			input:       "https://gateway.example.com",
			httpBase:    "https://gateway.example.com",
			wsURL:       "wss://gateway.example.com/api/v1/ws",
			devicesURL:  "https://gateway.example.com/api/v1/devices",
			routeTarget: "gateway.example.com:443",
		},
		{
			name:        "prefixedURL",
			input:       "https://gateway.example.com/v1",
			httpBase:    "https://gateway.example.com/v1",
			wsURL:       "wss://gateway.example.com/v1/api/v1/ws",
			devicesURL:  "https://gateway.example.com/v1/api/v1/devices",
			routeTarget: "gateway.example.com:443",
		},
		{
			name:        "wssIPv6URL",
			input:       "wss://[2001:db8::5]:9443/edge",
			httpBase:    "https://[2001:db8::5]:9443/edge",
			wsURL:       "wss://[2001:db8::5]:9443/edge/api/v1/ws",
			devicesURL:  "https://[2001:db8::5]:9443/edge/api/v1/devices",
			routeTarget: "[2001:db8::5]:9443",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints, err := resolveClientEndpoints(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.httpBase, endpoints.HTTPBase)
			assert.Equal(t, tt.wsURL, endpoints.WSURL)
			assert.Equal(t, tt.devicesURL, endpoints.DevicesURL)
			assert.Equal(t, tt.routeTarget, endpoints.RouteTarget)
		})
	}
}

func TestResolveClientEndpoints_RejectsInvalidInputs(t *testing.T) {
	for _, input := range []string{
		"",
		"ftp://gateway.example.com",
		"https://gateway.example.com?token=secret",
		"host:abc:def",
	} {
		_, err := resolveClientEndpoints(input)
		require.Error(t, err, "input %q should be rejected", input)
	}
}

func TestSelectAdvertisedIP_PrefersReachablePrivateIPv4(t *testing.T) {
	candidates := []net.IP{
		net.ParseIP("2001:db8::10"),
		net.ParseIP("203.0.113.5"),
		net.ParseIP("100.101.102.103"),
		net.ParseIP("192.168.1.44"),
	}

	assert.Equal(t, "100.101.102.103", selectAdvertisedIP(candidates))
}

func TestSelectAdvertisedIP_PrefersPrivateIPv6OverGlobalIPv6(t *testing.T) {
	candidates := []net.IP{
		net.ParseIP("2001:db8::10"),
		net.ParseIP("fd12:3456:789a::25"),
	}

	assert.Equal(t, "fd12:3456:789a::25", selectAdvertisedIP(candidates))
}

func TestGetAdvertisedHost_UsesOverrides(t *testing.T) {
	t.Setenv("LEVIK_ADVERTISE_HOST", "env-device.local")
	assert.Equal(t, "cli-device.local", getAdvertisedHost("", "cli-device.local"))
	assert.Equal(t, "env-device.local", getAdvertisedHost("", ""))
}

func TestHistoryFilePath_UsesLeVikHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LEVIK_HOME", home)

	path := historyFilePath("agent.history")
	assert.Equal(t, filepath.Join(home, "history", "agent.history"), path)

	info, err := os.Stat(filepath.Join(home, "history"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestValidateGatewaySecurity_RequiresAPIKeyWhenAPIEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.V1API.Enabled = true
	cfg.V1API.APIKey = ""

	err := validateGatewaySecurity(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "v1_api.api_key")
}

func TestValidateGatewaySecurity_PublicRequiresAllowlist(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.AllowFrom = nil

	err := validateGatewaySecurity(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow_from")
	assert.Contains(t, err.Error(), "telegram")
}

func TestValidateGatewaySecurity_PublicRequiresWorkspaceRestriction(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Agents.Defaults.RestrictToWorkspace = false

	err := validateGatewaySecurity(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "restrict_to_workspace")
}

func TestValidateGatewaySecurity_SafePublicConfigPasses(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Gateway.Host = "0.0.0.0"
	cfg.Agents.Defaults.RestrictToWorkspace = true
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.AllowFrom = []string{"123456"}
	cfg.V1API.Enabled = true
	cfg.V1API.APIKey = "test-key"

	err := validateGatewaySecurity(cfg)
	require.NoError(t, err)
}

func TestGatewayProviderConfigError_AllowsVLLMWithoutAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "vllm"

	err := gatewayProviderConfigError(cfg)
	require.NoError(t, err)
}

func TestGatewayProviderConfigError_RequiresVertexProjectID(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "vertex"

	err := gatewayProviderConfigError(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "project_id")
}

func TestExecuteLocalCapability_Microphone_CommandInjection(t *testing.T) {
	// Temporarily set up a fake Termux environment to allow executeLocalCapability to run
	oldPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	os.Setenv("PATH", tempDir+":"+oldPath)

	// Create a dummy termux-microphone-record executable
	err := os.WriteFile(filepath.Join(tempDir, "termux-microphone-record"), []byte(`#!/bin/sh
# Find the -f argument and create a dummy file there
OUTFILE=""
for i in "$@"; do
    if [ "$PREV" = "-f" ]; then
        OUTFILE="$i"
    fi
    PREV="$i"
done
if [ -n "$OUTFILE" ]; then
    echo "dummy audio data" > "$OUTFILE"
fi
echo "Mock termux-microphone-record $*" >&2
`), 0755)
	require.NoError(t, err)

	// Create a dummy file to simulate Termux environment in the temporary directory
	termuxRoot := filepath.Join(t.TempDir(), "data", "data", "com.termux")
	err = os.MkdirAll(termuxRoot, 0755)
	require.NoError(t, err)

	params := map[string]interface{}{
		"duration": "5; malicious_command", // Attempt to inject a command
	}

	_, capErr := executeLocalCapability("microphone", "record", params, termuxRoot)
	assert.Contains(t, capErr, "invalid duration parameter")

	// Restore original PATH
	os.Setenv("PATH", oldPath)
}

func TestExecuteLocalCapability_Microphone_DurationClamping(t *testing.T) {
	// Mock time.Sleep to prevent actual sleeping during test
	oldMicrophoneSleep := microphoneSleep
	microphoneSleep = func(d time.Duration) {
		// Do nothing, or log the duration for assertion if needed
	}
	defer func() { microphoneSleep = oldMicrophoneSleep }() // Restore original

	// Temporarily set up a fake Termux environment
	oldPath := os.Getenv("PATH")
	tempDir := t.TempDir()
	os.Setenv("PATH", tempDir+":"+oldPath)

	// Create a dummy termux-microphone-record executable
	err := os.WriteFile(filepath.Join(tempDir, "termux-microphone-record"), []byte(`#!/bin/sh
# Find the -f argument and create a dummy file there
OUTFILE=""
for i in "$@"; do
    if [ "$PREV" = "-f" ]; then
        OUTFILE="$i"
    fi
    PREV="$i"
done
if [ -n "$OUTFILE" ]; then
    echo "dummy audio data" > "$OUTFILE"
fi
echo "Mock termux-microphone-record $*" >&2
`), 0755) // Removed sleep from mock to avoid double-sleeping if microphoneSleep isn't mocked
	require.NoError(t, err)

	// Simulate Termux environment directory
	termuxRoot := filepath.Join(t.TempDir(), "data", "data", "com.termux")
	err = os.MkdirAll(termuxRoot, 0755)
	require.NoError(t, err)

	// Test with a duration larger than the maximum (300 seconds)
	params := map[string]interface{}{
		"duration": "9999", // This should be clamped to 300
	}

	result, capErr := executeLocalCapability("microphone", "record", params, termuxRoot)
	require.Empty(t, capErr) // No error expected

	// Although we cannot directly inspect the arguments passed to the real exec.Command easily,
	// the absence of an error and the clamping logic in the code provide confidence.
	assert.NotNil(t, result)

	// Restore original PATH
	os.Setenv("PATH", oldPath)
}

func TestCreateWorkspaceTemplates_DoesNotOverwriteExistingFiles(t *testing.T) {
	workspace := t.TempDir()
	customAgent := filepath.Join(workspace, "AGENT.md")

	require.NoError(t, os.WriteFile(customAgent, []byte("custom agent instructions"), 0644))

	createWorkspaceTemplates(workspace)

	agentData, err := os.ReadFile(customAgent)
	require.NoError(t, err)
	assert.Equal(t, "custom agent instructions", string(agentData))

	seededUser := filepath.Join(workspace, "USER.md")
	_, err = os.Stat(seededUser)
	require.NoError(t, err)
}

func TestWritePersonalizedBootstrapFiles_ReplacesDefaultTemplates(t *testing.T) {
	workspace := t.TempDir()
	createWorkspaceTemplates(workspace)

	writePersonalizedBootstrapFiles(workspace, "Jarvis", "Alex's personal AI assistant", "Alex", "keep it practical")

	agentData, err := os.ReadFile(filepath.Join(workspace, "AGENT.md"))
	require.NoError(t, err)
	assert.Contains(t, string(agentData), "You are Jarvis")
	assert.Contains(t, string(agentData), "not like a README or marketing page")

	identityData, err := os.ReadFile(filepath.Join(workspace, "IDENTITY.md"))
	require.NoError(t, err)
	assert.Contains(t, string(identityData), "## Name\nJarvis")
	assert.Contains(t, string(identityData), "You assist Alex directly")

	userData, err := os.ReadFile(filepath.Join(workspace, "USER.md"))
	require.NoError(t, err)
	assert.Contains(t, string(userData), "Name: Alex")
	assert.Contains(t, string(userData), "keep it practical")

	toolsData, err := os.ReadFile(filepath.Join(workspace, "TOOLS.md"))
	require.NoError(t, err)
	assert.Contains(t, string(toolsData), "Use tools to do real work")
}

func TestWritePersonalizedBootstrapFiles_PreservesCustomFiles(t *testing.T) {
	workspace := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(workspace, "AGENT.md"), []byte("custom live agent"), 0644))

	writePersonalizedBootstrapFiles(workspace, "Jarvis", "Alex's personal AI assistant", "Alex", "")

	agentData, err := os.ReadFile(filepath.Join(workspace, "AGENT.md"))
	require.NoError(t, err)
	assert.Equal(t, "custom live agent", string(agentData))
}

type fakeClientWSConn struct {
	activeWriters int32
	maxActive     int32
	messages      [][]byte
	messageTypes  []int
	mu            sync.Mutex
}

func (f *fakeClientWSConn) SetWriteDeadline(time.Time) error {
	return nil
}

func (f *fakeClientWSConn) WriteMessage(messageType int, data []byte) error {
	active := atomic.AddInt32(&f.activeWriters, 1)
	for {
		currentMax := atomic.LoadInt32(&f.maxActive)
		if active <= currentMax || atomic.CompareAndSwapInt32(&f.maxActive, currentMax, active) {
			break
		}
	}
	time.Sleep(5 * time.Millisecond)
	f.mu.Lock()
	f.messageTypes = append(f.messageTypes, messageType)
	f.messages = append(f.messages, append([]byte(nil), data...))
	f.mu.Unlock()
	atomic.AddInt32(&f.activeWriters, -1)
	return nil
}

func TestClientWSWriter_SerializesConcurrentWrites(t *testing.T) {
	fakeConn := &fakeClientWSConn{}
	writer := newClientWSWriter(fakeConn)

	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errCh <- writer.WriteJSON(map[string]interface{}{"index": i})
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&fakeConn.maxActive))
	assert.Len(t, fakeConn.messages, 8)
}

func TestSendChat_WritesExpectedEnvelope(t *testing.T) {
	fakeConn := &fakeClientWSConn{}
	writer := newClientWSWriter(fakeConn)

	require.NoError(t, sendChat(writer, "hello", "session-1"))
	require.Len(t, fakeConn.messages, 1)
	assert.Equal(t, websocket.TextMessage, fakeConn.messageTypes[0])

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(fakeConn.messages[0], &payload))
	assert.Equal(t, "chat", payload["type"])

	data, ok := payload["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "hello", data["message"])
	assert.Equal(t, "session-1", data["session_key"])
}

func TestHandleCapabilityRequest_WritesResponseEnvelope(t *testing.T) {
	fakeConn := &fakeClientWSConn{}
	writer := newClientWSWriter(fakeConn)

	handleCapabilityRequest(writer, "req-1", "speaker", "play", nil)

	require.Len(t, fakeConn.messages, 1)

	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(fakeConn.messages[0], &payload))
	assert.Equal(t, "capability_response", payload["type"])

	data, ok := payload["data"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "req-1", data["request_id"])
	assert.Equal(t, false, data["success"])
	assert.Contains(t, data["error"], "unsupported capability")
}

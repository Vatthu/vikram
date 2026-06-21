package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/gorilla/websocket"
	"github.com/Vatthu/vikram/pkg/logger"
)
const (
	clientWSWriteWait = 10 * time.Second
	clientHTTPTimeout = 15 * time.Second
)

var microphoneSleep = time.Sleep

type clientWSWriteConn interface {
	WriteMessage(messageType int, data []byte) error
	SetWriteDeadline(t time.Time) error
}

type clientWSWriter struct {
	conn clientWSWriteConn
	mu   sync.Mutex
}

func newClientWSWriter(conn clientWSWriteConn) *clientWSWriter {
	return &clientWSWriter{conn: conn}
}

func (w *clientWSWriter) WriteJSON(payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return w.WriteMessage(websocket.TextMessage, data)
}

func (w *clientWSWriter) WriteMessage(messageType int, data []byte) error {
	if w == nil || w.conn == nil {
		return fmt.Errorf("websocket writer not initialized")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.conn.SetWriteDeadline(time.Now().Add(clientWSWriteWait)); err != nil {
		return err
	}
	return w.conn.WriteMessage(messageType, data)
}

func clientCmd() {
	server := ""
	apiKey := ""
	deviceName := ""
	message := ""
	advertiseHost := ""

	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--server", "-s":
			if i+1 < len(args) {
				server = args[i+1]
				i++
			}
		case "--api-key", "-k":
			if i+1 < len(args) {
				apiKey = args[i+1]
				i++
			}
		case "--name", "-n":
			if i+1 < len(args) {
				deviceName = args[i+1]
				i++
			}
		case "--advertise-host":
			if i+1 < len(args) {
				advertiseHost = args[i+1]
				i++
			}
		case "--debug", "-d":
			logger.SetLevel(logger.DEBUG)
			fmt.Println("🔍 Debug mode enabled")
		case "-m", "--message":
			if i+1 < len(args) {
				message = args[i+1]
				i++
			}
		}
	}

	if server == "" {
		fmt.Println("Usage: vikram client --server <host[:port]|url> [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --server, -s    Gateway address or URL (required)")
		fmt.Println("  --api-key, -k   API key for authentication")
		fmt.Println("  --name, -n      Device name (defaults to hostname)")
		fmt.Println("  --advertise-host Hostname/IP this device should publish to the gateway")
		fmt.Println("  --message, -m   Send a single message and exit")
		fmt.Println("  --debug, -d     Enable debug logging")
		fmt.Println()
		fmt.Println("Examples:")
		fmt.Println("  vikram client --server mypc.tail1234.ts.net:18791")
		fmt.Println("  vikram client --server https://gateway.example.com")
		fmt.Println("  vikram client --server 100.91.10.18:18791 --api-key mykey")
		fmt.Println("  vikram client --server https://example.com/v1 --advertise-host phone.local")
		fmt.Println("  vikram client -s 192.168.1.10:18791 -m \"Hello from my phone\"")
		os.Exit(1)
	}

	if deviceName == "" {
		deviceName, _ = os.Hostname()
	}

	endpoints, err := resolveClientEndpoints(server)
	if err != nil {
		fmt.Printf("Invalid gateway address: %v\n", err)
		os.Exit(1)
	}

	// Detect local capabilities.
	capabilities := detectCapabilities()

	deviceID := fmt.Sprintf("%s-%s-%s", deviceName, runtime.GOOS, runtime.GOARCH)

	fmt.Printf("%s Connecting to gateway at %s...\n", logo, endpoints.HTTPBase)

	// Build WebSocket URL — never append the API key as a query parameter
	// because URLs appear in server logs and shell history in plaintext.
	// The key is sent exclusively via the Authorization header below.
	wsURL := endpoints.WSURL

	header := http.Header{}
	if apiKey != "" {
		header.Set("Authorization", "Bearer "+apiKey)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		fmt.Printf("Error connecting to gateway: %v\n", err)
		fmt.Println("\nTroubleshooting:")
		fmt.Println("  - Is the gateway running? (vikram gateway)")
		fmt.Println("  - Is v1_api enabled in config? (\"v1_api\": {\"enabled\": true})")
		fmt.Println("  - Is the address correct?")
		fmt.Println("  - Check firewall / Tailscale connectivity")
		os.Exit(1)
	}
	defer conn.Close()
	wsWriter := newClientWSWriter(conn)
	httpClient := &http.Client{Timeout: clientHTTPTimeout}

	// Read welcome message to get client ID.
	var welcomeMsg struct {
		Type string                 `json:"type"`
		Data map[string]interface{} `json:"data"`
	}
	if err := conn.ReadJSON(&welcomeMsg); err != nil {
		fmt.Printf("Error reading welcome: %v\n", err)
		os.Exit(1)
	}
	wsClientID := ""
	wsRegisterToken := ""
	if welcomeMsg.Data != nil {
		if cid, ok := welcomeMsg.Data["client_id"].(string); ok {
			wsClientID = cid
		}
		if token, ok := welcomeMsg.Data["registration_token"].(string); ok {
			wsRegisterToken = token
		}
	}

	fmt.Printf("%s Connected! (client: %s)\n", logo, wsClientID)

	// Register this device with the gateway.
	registerURL := endpoints.DevicesURL
	regBody := map[string]interface{}{
		"id":                deviceID,
		"name":              deviceName,
		"host":              getAdvertisedHost(endpoints.RouteTarget, advertiseHost),
		"platform":          runtime.GOOS,
		"capabilities":      capabilities,
		"version":           version,
		"ws_client_id":      wsClientID,
		"ws_register_token": wsRegisterToken,
	}
	regData, _ := json.Marshal(regBody)

	regReq, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, registerURL, strings.NewReader(string(regData)))
	regReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		regReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	regResp, err := httpClient.Do(regReq)
	if err != nil {
		fmt.Printf("⚠ Could not register device: %v\n", err)
	} else {
		body, _ := io.ReadAll(io.LimitReader(regResp.Body, 1024))
		regResp.Body.Close()
		if regResp.StatusCode != http.StatusOK {
			fmt.Printf("⚠ Gateway rejected device registration (%s): %s\n", regResp.Status, strings.TrimSpace(string(body)))
		} else if len(capabilities) > 0 {
			fmt.Printf("✓ Device registered as %s (capabilities: %v)\n", deviceID, capabilities)
		} else {
			fmt.Printf("✓ Device registered as %s\n", deviceID)
		}
	}

	// Start background goroutine to handle incoming messages (including capability requests).
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responseCh := make(chan string, 16)

	go clientReadPump(ctx, conn, wsWriter, responseCh, capabilities)

	// Send periodic heartbeats.
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				msg := map[string]interface{}{"type": "ping", "timestamp": time.Now()}
				if err := wsWriter.WriteJSON(msg); err != nil {
					logger.DebugC("client", fmt.Sprintf("Heartbeat write failed: %v", err))
					cancel()
					_ = conn.Close()
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	if message != "" {
		// One-shot mode.
		if err := sendChat(wsWriter, message, "client:"+deviceID); err != nil {
			fmt.Printf("Error sending message: %v\n", err)
			return
		}
		select {
		case resp := <-responseCh:
			fmt.Printf("\n%s %s\n", logo, resp)
		case <-time.After(120 * time.Second):
			fmt.Println("Timeout waiting for response")
		}
	} else {
		// Interactive mode.
		fmt.Printf("%s Interactive mode (Ctrl+C to exit)\n\n", logo)
		clientInteractiveMode(wsWriter, responseCh, deviceID)
	}

	// Deregister on exit.
	deregURL := fmt.Sprintf("%s/%s", endpoints.DevicesURL, deviceID)
	deregReq, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete, deregURL, nil)
	if apiKey != "" {
		deregReq.Header.Set("Authorization", "Bearer "+apiKey)
	}
	if resp, err := httpClient.Do(deregReq); err == nil && resp != nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
	fmt.Println("\n✓ Disconnected from gateway")
}

func clientReadPump(ctx context.Context, conn *websocket.Conn, writer *clientWSWriter, responseCh chan<- string, capabilities []string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				logger.DebugC("client", fmt.Sprintf("Read error: %v", err))
			}
			return
		}

		var msg struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "chat_response":
			var resp struct {
				Response string `json:"response"`
			}
			if err := json.Unmarshal(msg.Data, &resp); err == nil {
				select {
				case responseCh <- resp.Response:
				default:
				}
			}

		case "capability_request":
			// Handle capability requests from the gateway.
			var req struct {
				RequestID  string                 `json:"request_id"`
				Capability string                 `json:"capability"`
				Action     string                 `json:"action"`
				Params     map[string]interface{} `json:"params"`
			}
			if err := json.Unmarshal(msg.Data, &req); err != nil {
				continue
			}
			go handleCapabilityRequest(writer, req.RequestID, req.Capability, req.Action, req.Params)

		case "pong":
			// Heartbeat acknowledged.

		case "error":
			var errMsg string
			json.Unmarshal(msg.Data, &errMsg)
			fmt.Printf("\n⚠ Server error: %s\n", errMsg)
		}
	}
}

func handleCapabilityRequest(writer *clientWSWriter, requestID, capability, action string, params map[string]interface{}) {
	logger.InfoCF("client", "Capability request received", map[string]interface{}{
		"request_id": requestID, "capability": capability, "action": action,
	})

	var result interface{}
	var capErr string

	switch capability {
	case "camera":
		result, capErr = executeLocalCapability("camera", action, params, "")
	case "microphone":
		result, capErr = executeLocalCapability("microphone", action, params, "")
	case "screen":
		result, capErr = executeLocalCapability("screen", action, params, "")
	default:
		capErr = fmt.Sprintf("unsupported capability: %s", capability)
	}

	resp := map[string]interface{}{
		"type": "capability_response",
		"data": map[string]interface{}{
			"request_id": requestID,
			"success":    capErr == "",
			"data":       result,
			"error":      capErr,
		},
		"timestamp": time.Now(),
	}
	data, _ := json.Marshal(resp)
	if err := writer.WriteMessage(websocket.TextMessage, data); err != nil {
		logger.DebugC("client", fmt.Sprintf("Capability response write failed: %v", err))
	}
}

func executeLocalCapability(capability, action string, params map[string]interface{}, termuxRootOverride string) (interface{}, string) {
	// Check if we're on Termux (Android).
	isTermux := false
	termuxPath := "/data/data/com.termux"
	if termuxRootOverride != "" {
		termuxPath = termuxRootOverride
	}
	if _, err := os.Stat(termuxPath); err == nil {
		isTermux = true
	}

	switch capability {
	case "camera":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_cap_%d.jpg", time.Now().UnixNano()))
			output, err := execCommand("termux-camera-photo", "-c", "0", outFile)
			if err != nil {
				return nil, fmt.Sprintf("camera capture failed: %v (%s)", err, output)
			}
			imgData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read capture: %v", err)
			}
			return map[string]interface{}{
				"format": "jpeg",
				"base64": base64Encode(imgData),
			}, ""
		}
		return nil, "camera not available on this platform without Termux"

	case "microphone":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_mic_%d.wav", time.Now().UnixNano()))
			duration := 5 // Default to 5 seconds
			if dStr, ok := params["duration"].(string); ok {
				parsedDuration, err := strconv.Atoi(dStr)
				if err != nil {
					return nil, fmt.Sprintf("invalid duration parameter: %v", err)
				}
				duration = parsedDuration
			}
			// Clamp duration to a reasonable maximum to prevent DoS (e.g., 5 minutes)
			if duration > 300 {
				duration = 300
			}

			if _, err := execCommand("termux-microphone-record", "-f", outFile, "-l", strconv.Itoa(duration)); err != nil {
				return nil, fmt.Sprintf("mic record failed: %v", err)
			}
			microphoneSleep(time.Duration(duration) * time.Second)
			execCommand("termux-microphone-record", "-q")
			audioData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read recording: %v", err)
			}
			return map[string]interface{}{
				"format": "wav",
				"base64": base64Encode(audioData),
			}, ""
		}
		return nil, "microphone not available on this platform without Termux"

	case "screen":
		if isTermux {
			outFile := filepath.Join(os.TempDir(), fmt.Sprintf("vikram_screen_%d.png", time.Now().UnixNano()))
			if _, err := execCommand("termux-screenshot", outFile); err != nil {
				return nil, fmt.Sprintf("screenshot failed: %v", err)
			}
			imgData, err := os.ReadFile(outFile)
			os.Remove(outFile)
			if err != nil {
				return nil, fmt.Sprintf("failed to read screenshot: %v", err)
			}
			return map[string]interface{}{
				"format": "png",
				"base64": base64Encode(imgData),
			}, ""
		}
		return nil, "screen capture not available on this platform"
	}

	return nil, fmt.Sprintf("unknown capability: %s", capability)
}

func execCommand(name string, arg ...string) (string, error) {
	out, err := exec.Command(name, arg...).CombinedOutput()
	return string(out), err
}

func base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func sendChat(writer *clientWSWriter, message, sessionKey string) error {
	msg := map[string]interface{}{
		"type": "chat",
		"data": map[string]interface{}{
			"message":     message,
			"session_key": sessionKey,
		},
		"timestamp": time.Now(),
	}
	return writer.WriteJSON(msg)
}

func clientInteractiveMode(writer *clientWSWriter, responseCh <-chan string, deviceID string) {
	prompt := fmt.Sprintf("%s You: ", logo)
	sessionKey := "client:" + deviceID

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          prompt,
		HistoryFile:     historyFilePath("client.history"),
		HistoryLimit:    100,
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})

	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		return
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				return
			}
			continue
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}
		if input == "exit" || input == "quit" {
			return
		}

		if err := sendChat(writer, input, sessionKey); err != nil {
			fmt.Printf("\n⚠ Error sending message: %v\n", err)
			return
		}

		select {
		case resp := <-responseCh:
			fmt.Printf("\n%s %s\n\n", logo, resp)
		case <-time.After(120 * time.Second):
			fmt.Println("\n⚠ Timeout waiting for response")
		}
	}
}

func detectCapabilities() []string {
	var caps []string

	// Check if on Termux (Android) with hardware access.
	isTermux := false
	if _, err := os.Stat("/data/data/com.termux"); err == nil {
		isTermux = true
	}

	if isTermux {
		// Check for termux-api commands.
		if _, err := exec.LookPath("termux-camera-photo"); err == nil {
			caps = append(caps, "camera")
		}
		if _, err := exec.LookPath("termux-microphone-record"); err == nil {
			caps = append(caps, "microphone")
		}
		if _, err := exec.LookPath("termux-media-player"); err == nil {
			caps = append(caps, "speaker")
		}
		if _, err := exec.LookPath("termux-screenshot"); err == nil {
			caps = append(caps, "screen")
		}
	} else {
		// Desktop detection.
		if _, err := exec.LookPath("ffmpeg"); err == nil {
			caps = append(caps, "camera")
			caps = append(caps, "microphone")
		}
		if _, err := exec.LookPath("arecord"); err == nil {
			caps = append(caps, "microphone")
		}
		if runtime.GOOS == "darwin" {
			// macOS always has screen capture via screencapture.
			caps = append(caps, "screen")
			caps = append(caps, "speaker")
		}
	}

	// Deduplicate.
	seen := make(map[string]bool)
	var unique []string
	for _, c := range caps {
		if !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}
	return unique
}

type clientEndpoints struct {
	HTTPBase    string
	WSURL       string
	DevicesURL  string
	RouteTarget string
}

func resolveClientEndpoints(server string) (clientEndpoints, error) {
	raw := strings.TrimSpace(server)
	if raw == "" {
		return clientEndpoints{}, fmt.Errorf("gateway address is empty")
	}

	if strings.Contains(raw, "://") {
		return resolveClientEndpointsFromURL(raw)
	}

	host, port, err := splitClientHostPort(raw)
	if err != nil {
		return clientEndpoints{}, err
	}

	hostPort := net.JoinHostPort(host, port)
	httpBase := fmt.Sprintf("http://%s", hostPort)
	return clientEndpoints{
		HTTPBase:    httpBase,
		WSURL:       fmt.Sprintf("ws://%s/api/v1/ws", hostPort),
		DevicesURL:  fmt.Sprintf("%s/api/v1/devices", httpBase),
		RouteTarget: hostPort,
	}, nil
}

func resolveClientEndpointsFromURL(raw string) (clientEndpoints, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return clientEndpoints{}, err
	}
	if u.Host == "" {
		return clientEndpoints{}, fmt.Errorf("gateway URL must include a host")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return clientEndpoints{}, fmt.Errorf("gateway URL must not include query parameters or fragments")
	}

	httpScheme, wsScheme, err := clientTransportSchemes(strings.ToLower(u.Scheme))
	if err != nil {
		return clientEndpoints{}, err
	}

	pathPrefix := strings.TrimRight(u.EscapedPath(), "/")
	httpBase := fmt.Sprintf("%s://%s%s", httpScheme, u.Host, pathPrefix)
	return clientEndpoints{
		HTTPBase:    httpBase,
		WSURL:       fmt.Sprintf("%s://%s%s/api/v1/ws", wsScheme, u.Host, pathPrefix),
		DevicesURL:  fmt.Sprintf("%s/api/v1/devices", httpBase),
		RouteTarget: routeTargetForURL(u, httpScheme),
	}, nil
}

func clientTransportSchemes(scheme string) (httpScheme string, wsScheme string, err error) {
	switch scheme {
	case "http", "ws":
		return "http", "ws", nil
	case "https", "wss":
		return "https", "wss", nil
	default:
		return "", "", fmt.Errorf("unsupported gateway URL scheme %q", scheme)
	}
}

func splitClientHostPort(raw string) (string, string, error) {
	if raw == "" {
		return "", "", fmt.Errorf("gateway address is empty")
	}

	if strings.HasPrefix(raw, "[") {
		host, port, err := net.SplitHostPort(raw)
		if err != nil {
			return "", "", fmt.Errorf("invalid gateway address %q", raw)
		}
		return host, port, nil
	}

	if ip := net.ParseIP(raw); ip != nil {
		return raw, "18791", nil
	}

	if host, port, err := net.SplitHostPort(raw); err == nil {
		return host, port, nil
	}

	if !strings.Contains(raw, ":") {
		return raw, "18791", nil
	}

	return "", "", fmt.Errorf("invalid gateway address %q", raw)
}

func routeTargetForURL(u *url.URL, httpScheme string) string {
	if u == nil {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}

	port := "80"
	if httpScheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func getAdvertisedHost(routeTarget string, override string) string {
	if host := strings.TrimSpace(override); host != "" {
		return host
	}
	if host := strings.TrimSpace(os.Getenv("VIKRAM_ADVERTISE_HOST")); host != "" {
		return host
	}
	if host := advertisedHostFromRoute(routeTarget); host != "" {
		return host
	}
	return advertisedHostFromInterfaces()
}

func advertisedHostFromRoute(routeTarget string) string {
	if strings.TrimSpace(routeTarget) == "" {
		return ""
	}

	conn, err := net.Dial("udp", routeTarget)
	if err != nil {
		return ""
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || !isAdvertisableIP(localAddr.IP) {
		return ""
	}
	return localAddr.IP.String()
}

func advertisedHostFromInterfaces() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var candidates []net.IP
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromNetAddr(addr)
			if isAdvertisableIP(ip) {
				candidates = append(candidates, ip)
			}
		}
	}

	return selectAdvertisedIP(candidates)
}

func ipFromNetAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func isAdvertisableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !ip.IsLoopback() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast()
}

func selectAdvertisedIP(candidates []net.IP) string {
	bestRank := 999
	bestIP := ""
	for _, ip := range candidates {
		rank := advertiseIPRank(ip)
		if rank < bestRank || (rank == bestRank && ip.String() < bestIP) {
			bestRank = rank
			bestIP = ip.String()
		}
	}
	return bestIP
}

func advertiseIPRank(ip net.IP) int {
	if !isAdvertisableIP(ip) {
		return 999
	}

	if ip4 := ip.To4(); ip4 != nil {
		switch {
		case ip4.IsPrivate() || isCarrierGradeNAT(ip4):
			return 0
		case ip4.IsGlobalUnicast():
			return 1
		default:
			return 4
		}
	}

	switch {
	case ip.IsPrivate():
		return 2
	case ip.IsGlobalUnicast():
		return 3
	default:
		return 4
	}
}

func isCarrierGradeNAT(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127
}

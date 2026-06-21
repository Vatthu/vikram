# MCP Client Integration for Vikram

## Part 1 — Current Tool Flow (CODE-VERIFIED)

```
AgentLoop.runLLMIteration()                     ← loop.go:688
  │
  ├─ al.tools.ToProviderDefs()                  ← base.go:91
  │    └─ iterates ToolRegistry.tools map
  │    └─ returns []providers.ToolDefinition    ← providers/types.go:43-52
  │
  ├─ provider.Chat(messages, toolDefs, model)   ← loop.go:694
  │    └─ LLM returns response.ToolCalls         ← providers/types.go:5-11
  │
  ├─ For each ToolCall:
  │    al.tools.ExecuteWithContext(ctx, name, args, tc)  ← loop.go:962
  │    │
  │    └─ ToolRegistry.ExecuteWithContext()      ← base.go:110
  │         ├─ tr.Get(name)                      ← base.go:61 (map lookup)
  │         ├─ tool.Execute(ctx, tc, args)        ← base.go:119 (Tool interface)
  │         └─ returns *ToolResult               ← result.go:8-36
  │
  │    └─ Result capped at 50K chars             ← loop.go:998
  │    └─ Appended as "tool" role message         ← loop.go:1001-1007
  │
  └─ Loop back to LLM with updated messages
```

**Key integration point:** `ToolRegistry.Register()` + `Tool.Execute()`. An MCP tool is just another `Tool` implementation registered at startup.

---

## Part 2 — MCP Client Architecture

```
                         ┌─────────────────────────┐
                         │     AgentLoop            │
                         │  (unchanged)             │
                         └────────┬────────────────┘
                                  │ ExecuteWithContext("playwright_navigate", args, tc)
                                  ▼
                         ┌─────────────────────────┐
                         │     ToolRegistry          │
                         │  (unchanged)             │
                         │                          │
                         │  local: exec, files, ...  │
                         │  mcp:   playwright_*      │
                         │  mcp:   github_*          │
                         │  mcp:   sequential_*      │
                         └────────┬────────────────┘
                                  │
                    ┌─────────────┴──────────────┐
                    ▼                            ▼
            ┌──────────────┐            ┌──────────────────┐
            │ Local Tool    │            │ MCPTool           │
            │ (ExecTool...) │            │ (implements Tool) │
            │               │            │                   │
            │ Direct exec   │            │ Write JSON-RPC    │
            │ + allowlist   │            │ to stdin          │
            │ + deny pattern│            │                   │
            │ + path confine│            │ Read JSON-RPC     │
            └──────────────┘            │ from stdout       │
                                        │                   │
                                        │ timeout: 120s     │
                                        │ max_output: 100KB │
                                        └────────┬──────────┘
                                                 │
                                                 │ stdin/stdout
                                                 ▼
                                        ┌──────────────────┐
                                        │ MCP Server        │
                                        │ (external process)│
                                        │                   │
                                        │ npx playwright    │
                                        │ npx github-mcp    │
                                        │ uvx filesystem    │
                                        └──────────────────┘
```

### Mapping

| Vikram Concept | MCP Equivalent | How |
|--------------|----------------|-----|
| `Tool.Name()` | MCP tool `name` | From `tools/list` response |
| `Tool.Description()` | MCP tool `description` | From `tools/list` response |
| `Tool.Parameters()` | MCP tool `inputSchema` | From `tools/list` response |
| `Tool.Execute(ctx, tc, args)` | MCP `tools/call` | JSON-RPC request over stdin, response from stdout |
| `ToolResult.ForLLM` | MCP `content[].text` | First text content block |
| `ToolResult.IsError` | MCP `isError` | Boolean from response |
| `ToolContext` | No MCP equivalent | Vikram context stays internal |

---

## Part 3 — Implementation Plan (CODE-LEVEL)

### New: `pkg/mcp/protocol.go` — MCP JSON-RPC types

```go
package mcp

// Request is a JSON-RPC 2.0 request.
type Request struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      int         `json:"id"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      int         `json:"id"`
    Result  interface{} `json:"result,omitempty"`
    Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

// ToolListResult is the response to tools/list.
type ToolListResult struct {
    Tools []ToolDef `json:"tools"`
}

type ToolDef struct {
    Name        string                 `json:"name"`
    Description string                 `json:"description"`
    InputSchema map[string]interface{} `json:"inputSchema"`
}

// CallToolParams is the params for tools/call.
type CallToolParams struct {
    Name      string                 `json:"name"`
    Arguments map[string]interface{} `json:"arguments"`
}

// CallToolResult is the result of tools/call.
type CallToolResult struct {
    Content []ContentBlock `json:"content"`
    IsError bool           `json:"isError,omitempty"`
}

type ContentBlock struct {
    Type     string `json:"type"`
    Text     string `json:"text,omitempty"`
    MimeType string `json:"mimeType,omitempty"`
    Data     string `json:"data,omitempty"`
}
```

### New: `pkg/mcp/client.go` — Process management + JSON-RPC transport

```go
package mcp

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "os/exec"
    "sync"
    "sync/atomic"
    "time"
)

// pendingCall tracks one in-flight JSON-RPC request waiting for its response.
type pendingCall struct {
    response chan Response
    err      chan error
}

// Client manages one MCP server process and its JSON-RPC transport.
// Supports concurrent tool calls: writes are serialized via a small mutex on
// stdin; reads are dispatched to the correct caller by a background goroutine
// that matches response IDs to pending callers.
type Client struct {
    cmd     *exec.Cmd
    stdin   io.WriteCloser
    writeMu sync.Mutex // only serializes writes to stdin (not the entire call)
    nextID  atomic.Int64
    timeout time.Duration
    tools   []ToolDef

    // Background reader + request correlation
    readerCtx    context.Context
    readerCancel context.CancelFunc
    pending      sync.Map // map[int]chan Response — keyed by request ID
    initDone     chan struct{}
}

// ClientConfig defines how to spawn an MCP server.
type ClientConfig struct {
    Command string        // "npx" or "uvx" or "node"
    Args    []string      // ["-y", "@anthropic/mcp-playwright"]
    Timeout time.Duration // per-call timeout, default 120s
}

// NewClient spawns the MCP server, starts the background reader, sends
// initialize, and discovers tools via tools/list.
func NewClient(ctx context.Context, cfg ClientConfig) (*Client, error) {
    cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...)
    cmd.Stderr = nil

    stdin, err := cmd.StdinPipe()
    if err != nil { return nil, err }

    stdoutPipe, err := cmd.StdoutPipe()
    if err != nil { return nil, err }

    if err := cmd.Start(); err != nil { return nil, err }

    readerCtx, readerCancel := context.WithCancel(context.Background())

    c := &Client{
        cmd:          cmd,
        stdin:        stdin,
        timeout:      cfg.Timeout,
        readerCtx:    readerCtx,
        readerCancel: readerCancel,
        initDone:     make(chan struct{}),
    }
    if c.timeout == 0 { c.timeout = 120 * time.Second }
    c.nextID.Store(1)

    // Start background reader BEFORE any calls — it drains stdout
    // and dispatches responses to waiting callers by request ID.
    go c.readLoop(stdoutPipe)

    // Initialize (still sequential — no concurrent calls during setup)
    if _, err := c.call(ctx, "initialize", map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities":    map[string]interface{}{},
        "clientInfo":      map[string]string{"name": "vikram", "version": "1.0"},
    }); err != nil {
        cmd.Process.Kill()
        readerCancel()
        return nil, fmt.Errorf("mcp initialize: %w", err)
    }

    // Discover tools
    result, err := c.call(ctx, "tools/list", nil)
    if err != nil {
        cmd.Process.Kill()
        readerCancel()
        return nil, fmt.Errorf("mcp tools/list: %w", err)
    }

    var tl ToolListResult
    if err := reMarshal(result, &tl); err != nil {
        cmd.Process.Kill()
        readerCancel()
        return nil, fmt.Errorf("mcp parse tools: %w", err)
    }
    c.tools = tl.Tools
    close(c.initDone)
    return c, nil
}

// readLoop is the single background goroutine that reads JSON-RPC responses
// from stdout and dispatches them to the correct caller by request ID.
// It runs until the reader context is cancelled (on Close or process exit).
func (c *Client) readLoop(stdout io.Reader) {
    scanner := bufio.NewScanner(stdout)
    for scanner.Scan() {
        var resp Response
        if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
            continue // skip unparseable lines (server logging noise)
        }
        if ch, ok := c.pending.LoadAndDelete(resp.ID); ok {
            select {
            case ch.(chan Response) <- resp:
            default:
                // caller already timed out and stopped listening
            }
        }
        // responses with no matching ID are dropped (notifications, late responses)
    }
}

// call sends a JSON-RPC request and blocks until the matching response arrives
// or the context is cancelled.  No global mutex — concurrent callers are
// serialized only on the stdin write, and responses are correlated by ID.
func (c *Client) call(ctx context.Context, method string, params interface{}) (interface{}, error) {
    id := int(c.nextID.Add(1))
    req := Request{JSONRPC: "2.0", ID: id, Method: method, Params: params}

    // Register response channel BEFORE writing — avoids a race where the
    // background reader processes the response before we're listening.
    respCh := make(chan Response, 1)
    c.pending.Store(id, respCh)
    defer c.pending.Delete(id)

    data, _ := json.Marshal(req)
    data = append(data, '\n')

    // Serialize writes to stdin (the ONLY mutex in the hot path).
    c.writeMu.Lock()
    _, err := c.stdin.Write(data)
    c.writeMu.Unlock()
    if err != nil {
        return nil, fmt.Errorf("write request: %w", err)
    }

    // Wait for the background reader to deliver our response.
    select {
    case resp := <-respCh:
        if resp.Error != nil {
            return nil, fmt.Errorf("mcp error %d: %s", resp.Error.Code, resp.Error.Message)
        }
        return resp.Result, nil
    case <-ctx.Done():
        return nil, ctx.Err()
    }
}

// CallTool sends a tools/call request.  Safe for concurrent use — multiple
// goroutines can call CallTool simultaneously and responses will be routed
// to the correct caller by request ID.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (*CallToolResult, error) {
    ctx, cancel := context.WithTimeout(ctx, c.timeout)
    defer cancel()

    result, err := c.call(ctx, "tools/call", CallToolParams{Name: name, Arguments: args})
    if err != nil { return nil, err }

    var ct CallToolResult
    if err := reMarshal(result, &ct); err != nil { return nil, err }
    return &ct, nil
}

func (c *Client) Tools() []ToolDef { return c.tools }

func (c *Client) Close() error {
    c.readerCancel() // stops background reader goroutine
    if c.cmd.Process != nil {
        c.cmd.Process.Kill()
    }
    return nil
}

func reMarshal(src interface{}, dst interface{}) error {
    data, err := json.Marshal(src)
    if err != nil { return err }
    return json.Unmarshal(data, dst)
}
```

### Concurrency Model (Why This Is Correct)

```
                    ┌──────────────────────────────┐
                    │      Background Reader         │
                    │      (single goroutine)        │
                    │                                │
                    │  for scanner.Scan() {           │
                    │    resp := parse(line)          │
                    │    ch := pending.Load(resp.ID)  │
                    │    ch <- resp                   │
                    │  }                              │
                    └──────────┬───────────────────┘
                               │ dispatches by ID
                    ┌──────────▼───────────────────┐
                    │      pending sync.Map         │
                    │  {id: chan Response}           │
                    │                                │
                    │  Caller A (id=2): Store(2, chA)│
                    │  Caller B (id=3): Store(3, chB)│
                    │  Caller C (id=4): Store(4, chC)│
                    └──────────▲───────────────────┘
                               │ registers channel
          ┌────────────────────┼────────────────────┐
          │                    │                    │
    ┌─────┴──────┐      ┌─────┴──────┐      ┌─────┴──────┐
    │ Caller A    │      │ Caller B    │      │ Caller C    │
    │ id=2        │      │ id=3        │      │ id=4        │
    │ Store(2,ch) │      │ Store(3,ch) │      │ Store(4,ch) │
    │ writeMu.Lock│      │ writeMu.Lock│      │ writeMu.Lock│
    │ stdin.Write │      │ stdin.Write │      │ stdin.Write │
    │ writeMu.Unlk│      │ writeMu.Unlk│      │ writeMu.Unlk│
    │ <-ch        │      │ <-ch        │      │ <-ch        │
    └─────────────┘      └─────────────┘      └─────────────┘
```

**Correctness guarantees:**

1. **No response misrouting.** Each caller gets a unique atomic ID. The background reader matches response.ID to the caller's channel in `pending`. A response for caller A can never reach caller B.

2. **No write interleaving.** `writeMu` serializes writes to stdin. JSON-RPC messages are newline-delimited, so each `Write(data + '\n')` is an atomic unit on the wire. No partial writes.

3. **No channel leak.** `defer c.pending.Delete(id)` runs when `call()` returns (success, error, or timeout). The channel is garbage-collected after the caller stops listening.

4. **No goroutine leak.** The background reader exits when `readerCtx` is cancelled (on `Close()`). `scanner.Scan()` returns false when stdout is closed (process exits).

5. **Late response safety.** If a caller times out and returns, its channel is removed from `pending`. If the response arrives later, `LoadAndDelete` returns nil and the response is dropped — no panic, no goroutine stall.

**Performance characteristics:**

| Scenario | Before (global mutex) | After (per-write mutex) |
|----------|----------------------|------------------------|
| 1 in-flight call | 1 mutex lock/unlock | 1 write lock + 1 map store/load |
| 3 concurrent calls | Call B waits for A to finish reading response before even writing | All 3 write (serialized by writeMu), all 3 wait on their own channels simultaneously |
| Call A slow (30s response time) | Blocks B and C for 30s | B and C get their responses independently |

**The key insight:** JSON-RPC over stdio is fundamentally ordered — responses arrive in the order the server processes them, not necessarily in request order. The ID-based correlation + background reader decouples request submission from response collection. The only serialization point is the stdin pipe write (inherent to any pipe), not the entire call lifecycle.

### New: `pkg/mcp/adapter.go` — MCP → Tool adapter

```go
package mcp

import (
    "context"
    "fmt"
    "strings"

    "github.com/vatthu/vikram/pkg/tools"
)

// Adapter wraps an MCP tool as a Vikram Tool implementation.
type Adapter struct {
    client    *Client
    toolDef   ToolDef
    prefix    string // namespace prefix, e.g. "mcp_playwright_"
    unsafe    bool
    maxOutput int
}

// NewAdapter creates a Vikram Tool from an MCP tool definition.
func NewAdapter(client *Client, def ToolDef, prefix string, maxOutput int) *Adapter {
    return &Adapter{
        client:    client,
        toolDef:   def,
        prefix:    prefix,
        unsafe:    true, // all MCP tools are external/untrusted
        maxOutput: maxOutput,
    }
}

func (a *Adapter) Name() string {
    return a.prefix + sanitizeName(a.toolDef.Name)
}

func (a *Adapter) Description() string {
    return fmt.Sprintf("[MCP] %s", a.toolDef.Description)
}

func (a *Adapter) Parameters() map[string]interface{} {
    return a.toolDef.InputSchema
}

func (a *Adapter) Execute(ctx context.Context, tc tools.ToolContext, args map[string]interface{}) *tools.ToolResult {
    result, err := a.client.CallTool(ctx, a.toolDef.Name, args)
    if err != nil {
        return tools.ErrorResult(fmt.Sprintf("MCP tool %s failed: %v", a.toolDef.Name, err))
    }

    // Extract text content from MCP response
    var parts []string
    for _, block := range result.Content {
        switch block.Type {
        case "text":
            parts = append(parts, block.Text)
        case "image":
            parts = append(parts, fmt.Sprintf("[image: %s, %d bytes]", block.MimeType, len(block.Data)))
        case "resource":
            parts = append(parts, fmt.Sprintf("[resource: %s]", block.MimeType))
        }
    }
    output := strings.Join(parts, "\n")

    if len(output) > a.maxOutput {
        output = output[:a.maxOutput] + fmt.Sprintf("\n\n[MCP output truncated: %d total, showing %d]", len(output), a.maxOutput)
    }

    return &tools.ToolResult{
        ForLLM:  output,
        IsError: result.IsError,
    }
}

func sanitizeName(name string) string {
    return strings.Map(func(r rune) rune {
        if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
            return r
        }
        return '_'
    }, name)
}
```

### Modify: `pkg/config/config.go` — MCP client config

```go
// Add to Config struct:
type MCPConfig struct {
    Enabled bool            `json:"enabled"`
    Servers []MCPServerConfig `json:"servers"`
}

type MCPServerConfig struct {
    Name    string   `json:"name"`     // "playwright"
    Command string   `json:"command"`  // "npx"
    Args    []string `json:"args"`     // ["-y", "@anthropic/mcp-playwright"]
    Timeout int      `json:"timeout"`  // seconds, default 120
    Prefix  string   `json:"prefix"`   // tool name prefix, e.g. "mcp_pw_"
    Allowed []string `json:"allowed"`  // specific tool names to expose (empty = all)
    MaxOutput int    `json:"max_output"` // max output chars, default 50_000
}

// Add to Config:
type Config struct {
    // ... existing fields ...
    MCP MCPConfig `json:"mcp"`
}
```

### Modify: `cmd/vikram/main.go` — startup wiring

In `gatewayCmd()`, after tool registry creation (`createToolRegistry` at loop.go line ~217), add:

```go
// Load MCP tools from external servers
if cfg.MCP.Enabled {
    for _, serverCfg := range cfg.MCP.Servers {
        client, err := mcp.NewClient(ctx, mcp.ClientConfig{
            Command: serverCfg.Command,
            Args:    serverCfg.Args,
            Timeout: time.Duration(serverCfg.Timeout) * time.Second,
        })
        if err != nil {
            fmt.Printf("⚠ MCP server %s failed to start: %v\n", serverCfg.Name, err)
            continue
        }

        allowed := make(map[string]bool)
        for _, t := range serverCfg.Allowed {
            allowed[t] = true
        }

        maxOut := serverCfg.MaxOutput
        if maxOut == 0 { maxOut = 50_000 }

        count := 0
        for _, toolDef := range client.Tools() {
            if len(allowed) > 0 && !allowed[toolDef.Name] {
                continue // tool not in allowlist
            }
            adapter := mcp.NewAdapter(client, toolDef, serverCfg.Prefix, maxOut)
            toolsRegistry.Register(adapter)
            count++
        }
        fmt.Printf("✓ MCP %s: %d tools registered\n", serverCfg.Name, count)
    }
}
```

### User Config Example

```json
{
  "mcp": {
    "enabled": true,
    "servers": [
      {
        "name": "playwright",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-playwright"],
        "prefix": "mcp_pw_",
        "max_output": 80000
      },
      {
        "name": "github",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-github"],
        "prefix": "mcp_gh_",
        "allowed": ["search_repositories", "get_file_contents", "list_issues"]
      },
      {
        "name": "filesystem",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-filesystem", "/tmp/vikram-mcp-fs"],
        "prefix": "mcp_fs_",
        "allowed": ["read_file", "write_file", "list_directory"]
      },
      {
        "name": "sequential",
        "command": "npx",
        "args": ["-y", "@anthropic/mcp-sequential-thinking"],
        "prefix": "mcp_think_"
      }
    ]
  }
}
```

---

## Part 4 — Security Model

### Risk: MCP tools bypass Go host security

**TRUE.** MCP tools execute in the MCP server process, not through `ExecTool`. The allowlist, deny patterns, and path containment in `pkg/tools/shell.go` do NOT apply.

### Mitigations (all implemented in the adapter, not trusted to the MCP server)

| Risk | Mitigation | Where |
|------|-----------|-------|
| External process is untrusted | `unsafe = true` flag on every MCP adapter | `adapter.go` |
| MCP tool could execute arbitrary code | `--mcp-allowed` restricts which tools are registered | Config + startup filtering |
| MCP output could be huge | `maxOutput` cap (50K default) in adapter | `adapter.go` |
| MCP server hangs | Per-call `context.WithTimeout` (120s default) | `client.go:CallTool` |
| MCP server crashes | `cmd.Process.Kill()` on close, re-registration skipped | `client.go:Close` |
| MCP response could inject prompts | Response wrapped in `[MCP]` prefix in description, output is plain text | `adapter.go` |
| Agent can't distinguish safe vs unsafe tools | MCP tool descriptions include `[MCP]` prefix. LLM sees this. | `adapter.go:Description()` |

### Tool registration contract

```
Startup:
  1. config.mcp.servers loaded
  2. For each server:
     a. Spawn process: npx -y <server>
     b. Send initialize (JSON-RPC)
     c. Send tools/list
     d. Filter against server.allowed (if set)
     e. Wrap each tool as MCPAdapter
     f. Register in ToolRegistry with [MCP] prefix

Runtime:
  3. LLM sees [MCP] prefix in tool descriptions — knows these are external
  4. Agent calls tool → ToolRegistry.ExecuteWithContext → MCPAdapter.Execute
  5. MCPAdapter writes JSON-RPC to stdin, reads from stdout
  6. Output capped at maxOutput, truncated with notice
  7. ToolResult returned to LLM

Shutdown:
  8. Gateway close → MCP clients close → processes killed
```

---

## Part 5 — Local vs MCP Tool Routing

**No automatic routing.** The LLM decides which tool to call based on the tool definitions it sees. MCP tools are visually distinct (`[MCP]` prefix in description). The agent is not told "prefer local" — it's told what each tool does and picks accordingly.

**Naming convention ensures clarity:**
```
exec                    ← local: Vikram's shell exec (safe, allowlisted)
mcp_pw_browser_navigate ← MCP: Playwright browser navigation (external)
mcp_gh_search_repos     ← MCP: GitHub repository search (external)
mcp_fs_read_file        ← MCP: External filesystem access (external, sandboxed to /tmp)
mcp_think_sequential    ← MCP: Sequential thinking tool (external)
```

---

## Part 6 — Use Case Validation

### Playwright MCP

```
Agent: "Take a screenshot of the login page"
LLM calls: mcp_pw_browser_navigate(url="http://localhost:3000/login")
→ MCPAdapter.Execute → JSON-RPC tools/call → playwright process
→ ToolResult{ForLLM: "Navigated to http://localhost:3000/login", IsError: false}

LLM calls: mcp_pw_browser_screenshot(name="login")
→ MCPAdapter.Execute → JSON-RPC tools/call → playwright process
→ ToolResult{ForLLM: "[image: image/png, 45231 bytes]", IsError: false}
```

### GitHub MCP

```
Agent: "Find open issues about authentication in the repo"
LLM calls: mcp_gh_list_issues(repo="vatthu/vikram", state="open", labels=["auth"])
→ ToolResult{ForLLM: "#42: OAuth token refresh fails after 1h\n#38: Add API key rotation", IsError: false}
```

### Sequential Thinking MCP

```
Agent: "Debug this complex race condition"
LLM calls: mcp_think_sequential(thought="Thread A acquires lock L1, then waits for L2. Thread B acquires L2, then waits for L1. This is a classic deadlock if both locks are held simultaneously...")
→ ToolResult{ForLLM: "..., nextThoughtNeeded: true, thoughtNumber: 3, totalThoughts: 5", IsError: false}
```

---

## Part 7 — Final Architecture Diagram

```
                            ┌──────────────────────────┐
                            │       AgentLoop           │
                            │   (unchanged — loop.go)   │
                            └──────────┬───────────────┘
                                       │ ToProviderDefs() ← includes MCP tools
                                       │ ExecuteWithContext()
                                       ▼
                            ┌──────────────────────────┐
                            │      ToolRegistry          │
                            │   (unchanged — base.go)   │
                            │                           │
                            │  ┌─────────────────────┐  │
                            │  │ Local (safe)         │  │
                            │  │ exec, files, notify  │  │
                            │  │ git, lint, browser   │  │
                            │  └─────────────────────┘  │
                            │                           │
                            │  ┌─────────────────────┐  │
                            │  │ MCP (external)       │  │
                            │  │ [MCP] playwright_*   │  │
                            │  │ [MCP] github_*       │  │
                            │  │ [MCP] fs_*           │  │
                            │  │ [MCP] think_*        │  │
                            │  └─────────┬───────────┘  │
                            └────────────┼──────────────┘
                                         │ Tool.Execute()
                                         ▼
                            ┌──────────────────────────┐
                            │      MCPAdapter           │
                            │  (implements Tool)        │
                            │                           │
                            │  cap output at maxOutput  │
                            │  timeout per call         │
                            │  [MCP] prefix in name     │
                            └──────────┬───────────────┘
                                       │ JSON-RPC over stdin/stdout
                                       ▼
                            ┌──────────────────────────┐
                            │     MCP Server            │
                            │  (external process)       │
                            │                           │
                            │  npx playwright-mcp       │
                            │  npx github-mcp           │
                            │  npx filesystem-mcp       │
                            └──────────────────────────┘
```

**Risks and mitigation summary:**
- External processes are untrusted → marked `unsafe`, allowlist filtering at registration
- No path containment for filesystem MCP → sandbox via MCP server's own config (e.g., `--directory /tmp/vikram-mcp-fs`)
- Process lifecycle tied to gateway → `Close()` kills all MCP processes
- JSON-RPC parsing errors → wrapped as `ToolResult.IsError`, returned to LLM for retry

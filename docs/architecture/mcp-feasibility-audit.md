# MCP Feasibility Audit for Vikram

## Part 1 — Current Tool Architecture (CODE-VERIFIED)

### Tool Definition

```
Tool interface  (pkg/tools/base.go:32-37)
  ├── Name() string
  ├── Description() string
  ├── Parameters() map[string]interface{}     ← OpenAI function-calling schema
  └── Execute(ctx, ToolContext, args) *ToolResult
```

### Tool to LLM Pipeline

```
ToolRegistry.Register(tool)
  ↓
ToolRegistry.ToProviderDefs()                  ← converts to []providers.ToolDefinition
  ↓                                              (base.go:91, iterates registry, calls Parameters())
AgentLoop.runLLMIteration()                    ← loop.go:688
  ↓   messages + toolDefs → provider.Chat()
LLM returns response.ToolCalls
  ↓
ToolRegistry.ExecuteWithContext(name, args, tc) ← base.go:110
  ↓   looks up tool by name, calls tool.Execute()
ToolResult{ForLLM, ForUser, IsError, Async, ExitCode}
  ↓   appended to messages as "tool" role message
Loop back to LLM
```

### Host Execution Layer (CODE-VERIFIED)

The Go host exposes 21 HTTP+JSON endpoints over a Unix domain socket:

| Endpoint | Type | Side Effect |
|----------|------|-------------|
| `/v1/workspaces/provision` | workspace_write | Filesystem mutation |
| `/v1/git/worktrees/create` | repo_write | Git operation |
| `/v1/git/worktrees/remove` | repo_write | Git operation |
| `/v1/git/rollback` | repo_write | Git operation |
| `/v1/repos/inspect` | read_only | Repository analysis |
| `/v1/repos/discover-targets` | read_only | File discovery |
| `/v1/repos/discover-verification` | read_only | Test discovery |
| `/v1/repos/discover-lint` | read_only | Lint tool discovery |
| `/v1/repos/run-lint` | workspace_write | Lint execution |
| `/v1/files/read` | read_only | Bounded file read |
| `/v1/files/write` | workspace_write | Bounded file write |
| `/v1/files/replace` | workspace_write | Exact text replacement |
| `/v1/artifacts/write` | workspace_write | Artifact persistence |
| `/v1/artifacts/read` | read_only | Artifact retrieval |
| `/v1/exec` | varies | Shell execution (allowlist-enforced) |
| `/v1/browser/test` | workspace_write | Browser test via Playwright |
| `/v1/notify/telegram` | external | Channel notification |
| `/v1/agent/think` | external | LLM call via team role |
| `/v1/review/change` | external | LLM review via reviewer model |

Security enforcement: `ExecTool` (`pkg/tools/shell.go:36-75`) — allowlist middleware (48 safe commands for DefaultAllowlist, +8 for DevAllowlist), 13 deny-pattern regexes, path containment via `isWithinRoot()` (`orchestratorhost/server.go:1195`), `.git` traversal blocking (`resolveWorktreeFilePath`, line 1469).

---

## Part 2 — MCP Compatibility

### Can Vikram act as an MCP Client?

**Technically feasible.** An MCP client sends `tools/list`, `tools/call`, `resources/read` requests to an MCP server over stdio or SSE. Vikram's agent could be an MCP client that discovers external tools.

**No immediate value.** Vikram's tool set is fixed and internal. There are no external MCP servers to consume. Adding MCP client capability now would add transport complexity (stdio process management or SSE connection lifecycle) with zero new tools.

### Can Vikram act as an MCP Server?

**YES — with high architectural fit.** The orchestrator host already exposes tools as typed HTTP+JSON endpoints. MCP server would add a stdio/SSE transport layer that exposes the same tools using MCP's JSON-RPC protocol. External clients (Claude Desktop, Cursor, Continue.dev) could then use Vikram tools.

### Can Vikram act as Both?

**YES — but the client side has no immediate use case.** Server-first, client-later.

### Mapping Table (VERIFIED)

| Vikram Component | MCP Equivalent | Fit |
|----------------|----------------|-----|
| `Tool` interface (`base.go:32`) | MCP Tool definition | ✅ Identical pattern (name, description, inputSchema) |
| `ToolRegistry` (`base.go:40`) | MCP server tool list | ✅ `tools/list` maps to registry iteration |
| `Tool.Execute()` (`base.go:36`) | `tools/call` handler | ✅ args → result; MCP wraps in JSON-RPC |
| `ToolResult` (`result.go:8`) | MCP tool result content | ✅ `ForLLM` → text content, `IsError` → isError |
| `ToolContext` (`base.go:18`) | MCP request context | ⚠️ MCP has no equivalent; Vikram context is richer (bus, async, session) |
| `HostActionSpec` (`types.go:107`) | MCP tool annotations | ✅ SideEffectLevel, ApprovalPolicy map to MCP annotations |
| Unix socket HTTP+JSON | MCP stdio/SSE transport | ⚠️ Different transport, same function |
| Allowlist/deny patterns | MCP has no security layer | ❌ MCP is transport-only; Vikram's security is in the executor |
| `AgentThinkRequest` (`types.go:501`) | No MCP equivalent | — Role-based LLM routing is unique to Vikram |

---

## Part 3 — Integration Design

### Architecture Decision: MCP Server ONLY, alongside existing HTTP endpoints

**Where:** Go host (`pkg/orchestratorhost/`) — same package that already serves tools over Unix socket.

**How:** Add an MCP transport layer that serves the same tools over stdio. The MCP server reads JSON-RPC requests from stdin, dispatches to the same tool handlers, writes JSON-RPC responses to stdout.

### Minimal Viable Integration

```
External Client (Claude Desktop, Cursor)
  │
  │ stdio (JSON-RPC)
  ▼
┌─────────────────────────────────┐
│ MCP Server (NEW: pkg/mcp/)      │
│                                 │
│  handleRequest(method, params)  │
│    ├── "tools/list"  → iterates ToolRegistry, returns MCP tool defs
│    ├── "tools/call"  → ToolRegistry.ExecuteWithContext(name, args, tc)
│    └── "initialize"  → returns server capabilities
│                                 │
│  Transport: stdio (os.Stdin/os.Stdout) or SSE
└────────────┬────────────────────┘
             │
             ▼
┌─────────────────────────────────┐
│ Existing Tool Layer (UNCHANGED) │
│                                 │
│  ToolRegistry                   │
│  Tool.Execute()                 │
│  ExecTool (security middleware) │
│  Host action handlers           │
└─────────────────────────────────┘
```

### Required Code Changes

| File | Change |
|------|--------|
| `pkg/mcp/server.go` (NEW) | MCP JSON-RPC server: stdio reader, request router, response writer |
| `pkg/mcp/protocol.go` (NEW) | MCP types: Request, Response, Tool, ToolCall, InitializeResult |
| `pkg/mcp/tools.go` (NEW) | Adapter: Tool → MCP ToolDefinition, ToolResult → MCP CallToolResult |
| `cmd/vikram/main.go` | Add `--mcp` flag that starts gateway in MCP stdio mode instead of daemon mode |
| `pkg/orchestratorhost/server.go` | None — HTTP endpoints unchanged |
| `pkg/tools/base.go` | None — Tool interface unchanged |

### What MCP does NOT touch (by design)

- **Security middleware** — unchanged. MCP calls go through the same `ExecTool.Execute()` as HTTP calls.
- **Path containment** — unchanged. `isWithinRoot()` and `resolveWorktreeFilePath()` are called by the tool handlers, not the transport.
- **Agent loop** — unchanged. `AgentLoop.runLLMIteration()` continues using `ToolRegistry` directly.
- **Python orchestrator** — unchanged. Continues using Unix socket HTTP+JSON.

### Data Flow (MCP tools/call)

```
1. External client writes JSON-RPC to Vikram's stdin:
   {"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"exec","arguments":{"command":"go test ./..."}}}

2. MCP server reads from stdin, parses JSON-RPC

3. MCP server calls ToolRegistry.ExecuteWithContext("exec", {"command":"go test ./..."}, tc)
   → ExecTool.Execute()
   → allowlist check (DefaultAllowlist)
   → deny pattern check
   → parseCommandForDirectExecution()
   → exec.CommandContext().Run()
   → ToolResult{ForLLM: output, ExitCode: &code}

4. MCP server wraps ToolResult in MCP response:
   {"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"..."}],"isError":false}}

5. Written to stdout
```

---

## Part 4 — Security Analysis

### Does MCP weaken Vikram's safety model?

**NO — if the MCP transport sits ABOVE the existing tool layer.**

The security model lives in `ExecTool.Execute()` (`pkg/tools/shell.go`), not in the transport. The MCP server calls the same `Execute()` method as the HTTP handlers. Security is invariant to transport.

### Top 5 Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| **1. MCP stdio bypasses Unix socket isolation** — external client connects directly to Vikram process | Medium | MCP server runs inside the same process, calls same ToolRegistry. No new attack surface at the tool layer. Add `--mcp-allowed-tools` flag to restrict which tools MCP clients can access. |
| **2. No authentication on stdio** — any process that can spawn Vikram with `--mcp` gets full tool access | High | Add `VIKRAM_MCP_API_KEY` env var. MCP server validates `Authorization` in `initialize` params. Without key, only `tools/list` (discovery) is allowed. |
| **3. MCP client can call any tool** — 21 tools exposed without per-client filtering | Medium | `--mcp-allowed-tools=exec,files/read,files/write` flag. Default: read-only tools only (read, inspect, discover). Write tools require explicit opt-in. |
| **4. MCP responses leak file contents** — `files/read` returns source code via stdout | Medium | Same as existing `/v1/files/read` — already bounded (default 4000 chars, `maxReadBytes`). No new risk. |
| **5. MCP transport is visible to process monitor** — `ps aux` shows full command with API key if passed as flag | Low | Accept API key via env var only, not CLI flag. `VIKRAM_MCP_API_KEY=...` |

### Preservation of Existing Safety

All three safety layers remain intact:

```
MCP Client
  │
  ▼
MCP Server (NEW — transport only, no security logic)
  │
  ▼
Tool.Execute() (UNCHANGED — Tool interface)
  │
  ▼
ExecTool (UNCHANGED — allowlist, deny patterns, path containment)
  │
  ▼
exec.CommandContext (UNCHANGED — OS-level isolation)
```

---

## Part 5 — Tradeoffs

### Current Vikram Tools vs MCP-Based Tools

| Dimension | Current (HTTP+JSON over Unix socket) | MCP (stdio JSON-RPC) |
|-----------|--------------------------------------|----------------------|
| **Flexibility** | Fixed endpoints, typed Go structs | Dynamic tool discovery, self-describing |
| **Security** | Unix socket permissions (0600), allowlist, path containment | No built-in security; depends on process isolation |
| **Performance** | HTTP over Unix socket — minimal overhead | stdio — zero network overhead, but synchronous by default |
| **Complexity** | 21 handler functions, no protocol layer | JSON-RPC parsing, request/response matching, error codes |
| **Interop** | Only Vikram's Python orchestrator (same machine) | Any MCP client (Claude Desktop, Cursor, Continue, any language) |
| **Tool discovery** | Manual — handlers registered in code | Automatic — `tools/list` returns all available tools |
| **Streaming** | HTTP response bodies (can be large) | MCP supports streaming responses |
| **Process model** | Long-running daemon | Can be daemon OR one-shot (stdio closes → process exits) |

### When MCP adds value

- **External tool consumption:** Claude Desktop wants Vikram's exec/files/git tools → MCP server enables this
- **Tool discovery:** External client queries `tools/list` → gets Vikram's full capability surface without hardcoding endpoints
- **Interop with AI editors:** Cursor, Continue.dev, Zed can use Vikram as a tool backend

### When MCP adds overhead

- **Internal use:** Vikram's Python orchestrator already talks to Go host over Unix socket — adding MCP transport adds a protocol layer with no benefit
- **Fixed tool set:** Vikram's 21 tools don't change at runtime — dynamic discovery is unnecessary overhead
- **Security model:** Unix socket `chmod 0600` is simpler and more battle-tested than MCP's "trust the client process" model

---

## Part 6 — Final Verdict

### Should Vikram adopt MCP? **YES — PARTIAL (server-only)**

### Best approach: **Server-only, opt-in, read-only-by-default**

Add `vikram mcp` as a new CLI subcommand that starts Vikram in MCP stdio mode. This is separate from `vikram gateway` (the daemon) and `vikram agent` (the interactive CLI). The MCP server exposes Vikram's tools to external MCP clients without changing any internal architecture.

### What NOT to do

- Do NOT replace the Tool interface with MCP
- Do NOT add MCP client capability (no external tools to consume)
- Do NOT make the Python orchestrator use MCP (Unix socket is better for same-machine)
- Do NOT remove the HTTP endpoints (they serve the orchestrator and console)

### Implementation path

**Phase 1 — Minimal (1 file, ~200 lines)**
- `pkg/mcp/server.go` — stdio JSON-RPC server, `tools/list` + `tools/call` + `initialize`
- `cmd/vikram/main.go` — add `"mcp"` subcommand case
- READ-ONLY tools only (`files/read`, `repos/inspect`, `repos/discover-*`)
- `VIKRAM_MCP_API_KEY` env var for auth

**Phase 2 — Full tool access (opt-in)**
- `--mcp-allowed-tools` flag to whitelist write tools
- `resources/read` for artifact access
- MCP prompts for common task templates

**Phase 3 — Advanced (future)**
- MCP client capability for consuming external tools
- SSE transport for remote MCP access
- Tool result streaming for long-running tasks

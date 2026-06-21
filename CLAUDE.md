# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build, Test, and Run

```bash
make build          # Build for current platform → build/vikram-{platform}-{arch}
make run ARGS=""    # Build and run
make test           # Run all Go tests (go test ./...)
make vet            # Static analysis (go vet ./...)
make fmt            # Format Go code
make check          # deps + fmt + vet + test (full CI check)
make release-check  # Run pre-release checks via scripts/release_check.sh
make install        # Install binary to ~/.local/bin
```

Run a single test:
```bash
go test ./pkg/... -run TestName -v
```

## Architecture

Vikram is an autonomous AI engineering system with a **Go host layer** (gateway, permissions, secrets, workspace control, channels, execution) and a **Python orchestrator** (workflow graphs, model routing, approvals, artifacts). They communicate over a local Unix domain socket at `/tmp/vikramd.sock` via HTTP+JSON.

### Go CLI (cmd/vikram/)

Single binary with subcommands. Key entry points in `cmd/vikram/main.go`:
- `vikram agent` — interactive or one-shot chat with an LLM
- `vikram gateway` — starts the full daemon (agent loop, channels, cron, heartbeat, API server, orchestrator host, event router, job queue, device registry)
- `vikram client` — connects to a remote gateway via WebSocket
- `vikram onboard` / `vikram configure` / `vikram doctor` — setup and diagnostics

### Go Packages (pkg/)

| Package | Role |
|---|---|
| `agent` | Main agent loop: processes inbound messages, manages tool-use iteration, context building, summarization |
| `bus` | Internal message bus — `InboundMessage`/`OutboundMessage` channels between agent, channels, and tools |
| `providers` | LLM provider abstraction (`LLMProvider` interface) with implementations for Anthropic, OpenAI, OpenRouter, Groq, Vertex, Bedrock, Azure, Ollama, and others. GitHub Copilot config fields exist but the provider is intentionally unsupported until a safe bridge exists. |
| `channels` | Telegram and WhatsApp integrations. Inbound messages are published to the bus; outbound dispatcher reads bus subscriptions |
| `config` | JSON config file (~/.vikram/config.json) with env var overrides. Central `Config` struct covers workspace, agents, providers, channels, tools, heartbeat, permissions, V1 API, voice |
| `tools` | Tool implementations: shell exec, filesystem, web search, edit, notify, cron, delegate, spawn subagents. Registered in `ToolRegistry` |
| `orchestrator` / `orchestratorhost` | Shared types and Go-side HTTP server exposing host capabilities (workspace provision, git worktrees, file read/write/replace, exec, artifact store, Telegram notify) over Unix socket |
| `api` | V1 REST/WebSocket API server with auth, rate limiting, chat, status, events, device registration |
| `events` | Event router with subscribe/emit pattern |
| `queue` | Job queue for deferred work |
| `cron` | Scheduled job service using cron expressions or interval-based triggers |
| `heartbeat` | Periodic proactive check-in service |
| `proactive` | Proactive suggestion engine with SOP framework |
| `permissions` | Hardware permission registry (camera, mic, SMS, location, etc.) — default-deny |
| `auth` | OAuth (device code + browser flow) and token-based auth for OpenAI/Anthropic |
| `epistemology` | Knowledge store backed by SQLite |
| `session` | Conversation session manager with history and summarization |
| `state` | User state tracking (last active channel, preferences) |
| `skills` | Skill installer and loader from GitHub repos |
| `sync` | Device registry for multi-device sync |
| `logger` | Structured logging with levels |

### Python Orchestrator (services/orchestrator/)

Fresh implementation using LangGraph for workflow graphs:
- `main.py` — FastAPI server entrypoint
- `workflow.py` — LangGraph workflow construction
- `models.py` — Shared type definitions mirroring `pkg/orchestrator/types.go`
- `host_client.py` — HTTP client calling Go's Unix socket API
- `policy.py` — Model capability routing and approval policy
- `store.py` — Task session persistence
- `settings.py` — Configuration

### Key Data Flow

1. User sends message via Telegram/WhatsApp/CLI → channel publishes `InboundMessage` to bus
2. Agent loop consumes from bus, runs LLM with tool-use loop, publishes response to bus
3. Channel outbound dispatcher delivers response back to user

For orchestrated tasks (Python orchestrator path):
1. Python receives task, calls Go host for workspace/worktree provisioning
2. Python inspects repo, discovers targets, reads files, plans changes
3. Python calls Go for file edits, exec, verification
4. Approval gates pause workflow for human Telegram review when needed
5. Artifacts (plans, implementations, review payloads) are persisted by Go, produced by Python

### Configuration

- Config file: `~/.vikram/config.json`
- Env var prefix: `VIKRAM_` (e.g., `VIKRAM_PROVIDERS_OPENAI_API_KEY`)
- `$VIKRAM_HOME` overrides the config directory
- `${VAR_NAME}` expansion supported in config JSON values
- Workspaces are sandboxed by default (`workspace.sandboxed: true`)

### Provider Handling

The `LLMProvider` interface (`pkg/providers/types.go`) is the single abstraction. `providers.CreateProvider(cfg)` resolves the configured provider and model. Provider selection is config-driven, not hardcoded — model assignment is dynamic based on capability, budget, and quality.

### Git Worktree Isolation

Orchestrated tasks execute in isolated git worktrees under `{workspace}/worktrees/{task_id}`. Go enforces that all file paths remain within the worktree root. The `orchestratorhost` package validates path containment and blocks `.git` traversal.

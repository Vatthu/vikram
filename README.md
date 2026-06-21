# Vikram

An autonomous AI engineering team that plans, implements, verifies, and reviews code on a dedicated machine. Go host layer for execution and safety. Python LangGraph orchestrator for workflow intelligence. Config-driven multi-agent team with per-role model assignment.

## Quick Start

```bash
make build          # Build for current platform
make test           # Run all tests
make run ARGS="agent -m 'your question'"  # Quick chat
```

```bash
vikram onboard       # Setup wizard
vikram doctor        # Health check
vikram agent         # Interactive chat
vikram gateway       # Start the full daemon
```

## Architecture

```
Gateway (Go daemon)              Orchestrator (Python)
├── Agent loop                   ├── LangGraph workflow (31 nodes)
├── Message bus                   ├── Adversarial spec validation
├── Channel adapters              ├── Lint → test → LLM judge pipeline
├── Tool execution (allowlisted)  ├── Git worktree isolation
├── Event router                  ├── Approval gates (Telegram)
├── Cron + Heartbeat              └── SQLite checkpointing
├── Subagent manager (multi-agent)
├── Orchestrator host (Unix socket)
├── API server (REST + WebSocket)
├── Management console (opt-in, 127.0.0.1:18793)
└── Dashboard (opt-in, 127.0.0.1:18792)
```

**Go owns the machine.** Shell execution, filesystem access, git operations, and notifications go through a security-allowlisted host daemon over a Unix socket.

**Python owns the workflow.** LangGraph state machine with checkpointing, adversarial spec validation, three-layer verification, and config-driven multi-agent routing.

## Team Configuration

```json
{
  "agents": {
    "list": [
      {"id": "lead", "role": "lead", "provider": "zhipu", "model": "glm-5.1", "max_tokens_per_day": 200000},
      {"id": "engineer", "role": "engineer", "provider": "deepseek", "model": "deepseek-chat", "max_tokens_per_day": 500000},
      {"id": "reviewer", "role": "reviewer", "provider": "mistral", "model": "mistral-small-latest", "max_tokens_per_day": 100000},
      {"id": "runner", "role": "runner", "provider": "nvidia", "model": "meta/llama-3.3-70b-instruct"}
    ]
  },
  "mcp": {
    "enabled": true,
    "servers": [
      {"name": "playwright", "command": "npx", "args": ["-y", "@anthropic/mcp-playwright"], "prefix": "mcp_pw_"}
    ]
  }
}
```

Each role gets its own provider and model. Config-driven, not hardcoded. Roles are assigned per workflow phase: lead plans, engineer implements, reviewer validates, runner tests.

## Provider Support

DeepSeek, Mistral, NVIDIA, OpenAI, Anthropic, Google Vertex AI, Gemini, OpenRouter, Groq, Zhipu (GLM), Ollama, Cerebras, SambaNova, Azure OpenAI, AWS Bedrock, GitHub Models, vLLM, Moonshot (Kimi), xAI (Grok), and more. GitHub Copilot config fields exist, but the provider is intentionally unsupported until a safe bridge is designed.

## Key Features

- **Multi-agent team** — config-driven role assignment with per-role models
- **Adversarial spec validation** — Devil's Advocate attacks the plan before code is written
- **Three-layer verification** — lint guard → test execution → independent LLM review
- **Transactional rollback** — managed task worktrees reset and clean back to HEAD on rejection
- **Cost enforcement** — per-agent daily token budgets with Telegram notifications
- **History compression** — context window management for long-running sessions
- **Observation shaping** — structured output templates guide model behavior
- **MCP client support** — experimental external MCP tool consumption with allowlist filtering
- **Management console** — opt-in local browser-based agent and provider management
- **Crash recovery** — checkpoint state before every LLM call, resume on restart

## Repository

```
cmd/vikram/          Go CLI entrypoints
pkg/                Go packages (agent, bus, providers, tools, orchestrator, ...)
services/           Python orchestrator (LangGraph)
docs/architecture/  Design documents and audit reports
research/           Upstream framework studies
contrib/            Launchd daemon config, channel adapters
```

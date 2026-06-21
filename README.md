<div align="center">

# Vikram

**Your AI engineering team that runs on your machine, with your keys, on your terms.**

[![CI](https://github.com/Vatthu/vikram/actions/workflows/ci.yml/badge.svg)](https://github.com/Vatthu/vikram/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Vatthu/vikram)](https://goreportcard.com/report/github.com/Vatthu/vikram)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/Vatthu/vikram)](go.mod)

[Install](#install) · [Quick Start](#quick-start) · [Why Vikram?](#why-vikram) · [Privacy](PRIVACY.md) · [Contributing](CONTRIBUTING.md)

</div>

---

## Why Vikram?

Every other AI coding tool either **sends your code to a cloud** or **locks you into one provider**. Vikram gives you full sovereignty:

| | Vikram | Cloud AI IDEs | Hosted AI Agents |
|---|:---:|:---:|:---:|
| 🔒 Code stays on your machine | ✅ | ❌ | ❌ |
| 🔑 Your own API keys (20+ providers) | ✅ | ❌ | Partial |
| 📡 Zero telemetry | ✅ | ❌ | ❌ |
| 💬 Multi-channel (CLI, Telegram, WhatsApp, REST API) | ✅ | ❌ | ❌ |
| 🛡️ Automatic provider fallback | ✅ | ❌ | ❌ |
| ⚡ Compiled binary — zero runtime deps | ✅ | ❌ | ❌ |
| 🧑‍💻 Fully open source | ✅ | Partial | Partial |

**[Read our Privacy Promise →](PRIVACY.md)**

## Install

```bash
# One-command install (macOS / Linux)
curl -sSL https://raw.githubusercontent.com/Vatthu/vikram/main/install.sh | sh

# Or build from source
git clone https://github.com/Vatthu/vikram.git
cd vikram && make build
```

## Quick Start

```bash
# First-time setup (2 minutes)
vikram onboard

# Verify everything works
vikram doctor

# Start chatting
vikram agent

# Or send a single message
vikram agent -m "explain this codebase"

# Start the full gateway daemon
vikram gateway
```

## Features

### 🤖 Multi-Agent Team
Config-driven role assignment with per-role models. Each agent gets its own provider, model, workspace, and token budget:

```json
{
  "agents": {
    "list": [
      {"id": "lead", "role": "lead", "provider": "gemini", "model": "gemini-2.5-flash"},
      {"id": "engineer", "role": "engineer", "provider": "deepseek", "model": "deepseek-chat"},
      {"id": "reviewer", "role": "reviewer", "provider": "anthropic", "model": "claude-sonnet-4-20250514"}
    ]
  }
}
```

### 🔗 20+ LLM Providers
OpenAI, Anthropic, Google Gemini, Google Vertex AI, DeepSeek, Mistral, NVIDIA, OpenRouter, Groq, Zhipu, Ollama, Cerebras, SambaNova, Azure OpenAI, AWS Bedrock, GitHub Models, vLLM, Moonshot, xAI, and more.

### 📡 Multi-Channel Communication
Talk to Vikram from anywhere:
- **CLI** — interactive terminal or single-shot commands
- **Telegram** — message your agent from your phone
- **WhatsApp** — via bridge adapter
- **REST API** — integrate with any system
- **WebSocket** — real-time streaming

### 🛡️ Council Fallback System
If your primary provider rate-limits or goes down, Vikram automatically falls back to your configured secondary provider. Zero downtime, zero manual intervention.

### 🔍 Adversarial Spec Validation
Before code is written, a Devil's Advocate attacks the plan. Three-layer verification catches issues: lint guard → test execution → independent LLM review.

### 💰 Token Budget Enforcement
Per-agent daily token budgets with real-time tracking. Configurable actions: notify via Telegram or hard-stop when budget is exceeded.

### 🧩 Skills & Tools
Extensible skill system for custom capabilities. Built-in tools for shell, filesystem, web search, code editing, and more. MCP client support for external tool servers.

## Architecture

```
Go Host (compiled binary)           Python Orchestrator (optional)
├── Agent Loop                      ├── LangGraph workflow
├── Message Bus                     ├── Adversarial validation
├── Channel Adapters                ├── Git worktree isolation
├── Tool Execution (sandboxed)      ├── Approval gates
├── Provider Router (20+)           └── SQLite checkpointing
├── Council Fallback
├── Cron + Heartbeat
├── Subagent Manager
├── REST API + WebSocket
└── Management Console
```

**Go owns the machine.** Shell execution, filesystem access, git operations — all through a security-sandboxed host daemon.

**Python owns the workflow.** LangGraph state machine with checkpointing, adversarial validation, and multi-agent routing.

## Project Structure

```
cmd/vikram/          CLI and gateway binary
pkg/                 Go packages (agent, tools, providers, bus, channels, ...)
services/            Python orchestrator (LangGraph)
docs/                Architecture and design documents
web/                 Web dashboard
workspace/           Default workspace templates
```

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, architecture overview, and PR workflow.

## Security

**Do not open public issues for security vulnerabilities.** See [SECURITY.md](SECURITY.md) for responsible disclosure.

## License

[MIT](LICENSE)

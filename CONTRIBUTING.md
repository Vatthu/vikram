# Contributing to Vikram

Thank you for considering contributing to Vikram! Every contribution helps make this project better for everyone.

## Development Setup

### Prerequisites

- **Go 1.24+** — [Install Go](https://go.dev/dl/)
- **Python 3.12+** — for the orchestrator service
- **Git** — for version control

### Quick Start

```bash
# Clone the repository
git clone https://github.com/Vatthu/vikram.git
cd vikram

# Build
make build

# Run tests
make test

# Run linter
make lint
```

### Project Structure

```
vikram/
├── cmd/vikram/          # Main binary — CLI, gateway, client
├── pkg/                 # Go packages (agent loop, tools, providers, etc.)
│   ├── agent/           # Core agent loop and LLM orchestration
│   ├── providers/       # LLM provider integrations (20+ providers)
│   ├── tools/           # Tool system (shell, filesystem, web, etc.)
│   ├── bus/             # Internal message bus
│   ├── channels/        # Telegram, WhatsApp, etc.
│   ├── config/          # Configuration management
│   ├── skills/          # Skill loading and management
│   └── ...
├── services/            # Python orchestrator
│   └── orchestrator/    # FastAPI-based task orchestration
├── docs/                # Documentation
├── web/                 # Web dashboard
└── workspace/           # Default workspace templates
```

### Architecture

Vikram uses a **split architecture**:

- **Go Host** (`cmd/vikram`) — compiled binary handling CLI, gateway, channels, LLM calls, and tool execution
- **Python Orchestrator** (`services/orchestrator`) — optional FastAPI service for complex multi-step task orchestration

Communication between Go Host and Python Orchestrator happens via Unix Domain Socket.

## How to Contribute

### Reporting Bugs

1. Check [existing issues](https://github.com/Vatthu/vikram/issues) first
2. Use the [bug report template](https://github.com/Vatthu/vikram/issues/new?template=bug_report.yml)
3. Include your `vikram version` output and relevant logs

### Suggesting Features

Use the [feature request template](https://github.com/Vatthu/vikram/issues/new?template=feature_request.yml).

### Submitting Code

1. **Fork** the repository
2. **Create a branch** from `main`: `git checkout -b fix/description`
3. **Make your changes** with clear commit messages
4. **Test**: `go test -race ./...`
5. **Lint**: `golangci-lint run` (must pass cleanly)
6. **Submit a PR** against `main`

### Commit Messages

Use clear, descriptive commit messages:

```
fix: prevent context compression on auth errors

The error classifier was matching "token" in auth errors like
"invalid token", triggering unnecessary context compression.
Switched to compound pattern matching.
```

### Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Run `golangci-lint` before submitting
- Add tests for new functionality
- Keep functions focused and under ~100 lines where practical
- Document exported types and functions

## Security

**Do not open public issues for security vulnerabilities.** Use [private vulnerability reporting](https://github.com/Vatthu/vikram/security/advisories/new) or email amit.vikramaditya@icloud.com.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).

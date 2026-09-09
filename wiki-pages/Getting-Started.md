# Getting Started

## Install

### One-command install

```bash
curl -sSL https://raw.githubusercontent.com/Vatthu/vikram/main/install.sh | sh
```

### Build from source

```bash
git clone https://github.com/Vatthu/vikram.git
cd vikram
make build
```

### Set up the Python orchestrator

```bash
cd services/orchestrator
python -m venv .venv
source .venv/bin/activate
pip install -e .
cd ../..
```

## First-time setup

```bash
vikram onboard
```

This runs an interactive wizard that:
1. Creates your workspace directory (`~/.vikram/`)
2. Asks which AI provider to use
3. Stores your API key
4. Validates the connection
5. Generates a default agent configuration

### Non-interactive setup (for scripts/CI)

```bash
vikram onboard --auto --provider anthropic --api-key "sk-ant-..."
```

## Verify installation

```bash
vikram doctor
```

This checks:
- Config file exists and is valid
- Workspace directory is writable
- Provider credentials are configured
- LLM connection works (makes a test call)
- Orchestrator is reachable (if running)

## Start the platform

```bash
vikram gateway
```

This starts both the Go host daemon and the Python orchestrator. The platform is now ready to accept tasks.

## Your first task

### Via CLI
```bash
vikram agent -m "Add input validation to the user registration endpoint"
```

### Via Telegram (if configured)
Send a message to your bot:
```
/task Add input validation to the user registration endpoint
```

### Via REST API
```bash
curl -X POST http://localhost:8080/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "my-first-task",
    "objective": "Add input validation to the user registration endpoint",
    "repo": {"path": "/path/to/your/repo", "default_branch": "main"}
  }'
```

## What happens next

1. Vikram creates an isolated git branch for the task
2. It plans the implementation approach
3. A different AI model attacks the plan looking for flaws
4. The plan is revised, then code is written
5. Tests run, lint checks pass
6. An independent model reviews the changes
7. You get a notification asking to approve
8. You review the diff, approve, and merge

The whole process typically takes 5–30 minutes depending on complexity.

## Next steps

- [Configuration Guide](Configuration-Guide) — tune your agent team
- [Submitting Tasks](Submitting-Tasks) — all the ways to give Vikram work
- [Telegram Setup](Telegram-Setup) — approve tasks from your phone

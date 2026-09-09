# Configuration Guide

Vikram's configuration lives in `~/.vikram/config.json`. You can edit it directly or use `vikram configure` for a guided experience.

## Agent Roster

The agent roster defines your team — which AI models handle which roles.

```json
{
  "agents": {
    "list": [
      {
        "id": "lead",
        "role": "lead",
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "capabilities": ["planning", "architecture"]
      },
      {
        "id": "engineer",
        "role": "engineer",
        "provider": "deepseek",
        "model": "deepseek-chat",
        "capabilities": ["implementation", "refactoring"]
      },
      {
        "id": "reviewer",
        "role": "reviewer",
        "provider": "openai",
        "model": "gpt-4o",
        "capabilities": ["code_review", "security"]
      },
      {
        "id": "qa",
        "role": "qa",
        "provider": "anthropic",
        "model": "claude-sonnet-4-20250514",
        "capabilities": ["testing", "browser_testing"]
      }
    ]
  }
}
```

### Roles

| Role | What it does |
|------|-------------|
| `lead` | Plans the approach, revises after adversarial review |
| `engineer` | Writes the implementation code |
| `reviewer` | Independent code review (different model from engineer) |
| `runner` | Executes verification commands |
| `qa` | Generates and runs browser/integration tests |

### Minimum viable config (one agent)

You can run with just one agent doing all roles:

```json
{
  "agents": {
    "defaults": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514"
    }
  }
}
```

## Provider Configuration

Each provider needs at minimum an API key:

```json
{
  "providers": {
    "anthropic": {
      "api_key": "sk-ant-..."
    },
    "openai": {
      "api_key": "sk-..."
    },
    "deepseek": {
      "api_key": "sk-..."
    }
  }
}
```

### Supported providers

OpenAI, Anthropic, Google Gemini, Google Vertex AI, DeepSeek, Mistral, OpenRouter, Groq, Ollama, NVIDIA, Cerebras, SambaNova, Azure OpenAI, AWS Bedrock, GitHub Models, xAI, Moonshot, and any OpenAI-compatible endpoint.

### Environment variables

API keys can reference environment variables:

```json
{
  "providers": {
    "anthropic": {
      "api_key": "${ANTHROPIC_API_KEY}"
    }
  }
}
```

## Council (Automatic Fallback)

If your primary provider goes down or rate-limits you, Vikram automatically falls back to a secondary:

```json
{
  "council": {
    "enabled": true,
    "primary_provider": "anthropic",
    "primary_model": "claude-sonnet-4-20250514",
    "fallback_provider": "openai",
    "fallback_model": "gpt-4o"
  }
}
```

The fallback activates on:
- 401/403 (auth errors) — infinite cooldown on primary
- 429 (rate limit) — 5-minute cooldown
- 503 (overloaded) — 1-minute cooldown

## Task Constraints

Set default constraints for all tasks:

```json
{
  "constraints": {
    "require_human_approval": true,
    "max_parallel_workers": 3,
    "max_cost_usd": 5.00,
    "allow_network": false
  }
}
```

| Field | Default | Meaning |
|-------|---------|---------|
| `require_human_approval` | `true` | Whether changes need your sign-off |
| `max_parallel_workers` | `1` | How many agents work in parallel within a task |
| `max_cost_usd` | `null` | Maximum spend per task (null = unlimited) |
| `allow_network` | `false` | Whether task execution can make outbound connections |

## Orchestrator Settings

The Python orchestrator has its own settings via environment variables:

| Variable | Default | Meaning |
|----------|---------|---------|
| `VIKRAM_AGENT_RETRY_COUNT` | `2` | Retries per agent call before fallback |
| `VIKRAM_AGENT_RETRY_BACKOFF_SECONDS` | `1.0` | Backoff between retries |
| `VIKRAM_PLAN_MIN_LINES` | `3` | Minimum plan content lines before implementation |

## Workspace Settings

```json
{
  "workspace": {
    "path": "~/.vikram/workspace",
    "sandboxed": true
  }
}
```

- `path` — where the platform stores state, worktrees, and artifacts
- `sandboxed` — restricts file access to the workspace directory

## Channel Configuration

See [Telegram Setup](Telegram-Setup) for the most common channel.

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "YOUR_BOT_TOKEN",
      "allow_from": ["YOUR_TELEGRAM_USER_ID"]
    }
  }
}
```

The `allow_from` field restricts who can send tasks to your bot. Without it, anyone who finds your bot could submit tasks.

# Submitting Tasks

There are four ways to give Vikram work.

## Via CLI (quickest for one-offs)

```bash
# One-shot task
vikram agent -m "Add rate limiting to the /api/v1/* endpoints"

# Interactive mode (for back-and-forth)
vikram agent
```

## Via Telegram (best for mobile)

Send a message to your configured bot:
```
Add rate limiting to the /api/v1/* endpoints
```

Or use the explicit task command:
```
/task Add rate limiting to the /api/v1/* endpoints
```

## Via REST API (for automation)

```bash
curl -X POST http://localhost:8080/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "task_id": "rate-limit-001",
    "source": "api",
    "requested_by": "founder",
    "objective": "Add rate limiting to the /api/v1/* endpoints",
    "repo": {
      "path": "/path/to/your/repo",
      "default_branch": "main"
    },
    "priority": "high",
    "constraints": {
      "require_human_approval": true,
      "max_cost_usd": 3.00
    }
  }'
```

## Via the Console (for visual management)

Open `http://localhost:8080/console` and use the task submission form.

## Task Options

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `task_id` | Yes | — | Unique ID (alphanumeric, dashes, underscores) |
| `objective` | Yes | — | What you want built (natural language) |
| `repo.path` | Yes | — | Absolute path to the git repository |
| `repo.default_branch` | No | `main` | Branch to base work on |
| `priority` | No | `normal` | `critical`, `high`, `normal`, or `low` |
| `constraints.max_cost_usd` | No | unlimited | Max spend for this task |
| `constraints.require_human_approval` | No | `true` | Override approval matrix |
| `constraints.max_parallel_workers` | No | `1` | Parallel agent execution |
| `depends_on` | No | `[]` | Task IDs that must complete first |
| `formation` | No | auto | Named formation to use |

## Writing Good Objectives

**Good objectives** are specific and actionable:
- "Add input validation to the CreateUser handler in pkg/api/users.go"
- "Write unit tests for the authentication middleware"
- "Refactor the database connection pool to use context-based timeouts"

**Weak objectives** are vague:
- "Make the code better" (better how?)
- "Fix bugs" (which bugs?)
- "Improve performance" (where? what's the target?)

The more specific your objective, the better the result. Include:
- Which files or packages are involved
- What the expected behavior should be
- Any constraints (don't break existing tests, maintain backward compatibility)

## Monitoring Progress

After submitting:
- **Console**: Real-time progress at `http://localhost:8080/console`
- **Telegram**: Phase transition notifications sent to your chat
- **API**: Poll `GET /v1/tasks/{task_id}` for status

## Task Lifecycle States

| Status | Meaning |
|--------|---------|
| `queued` | Waiting in the priority queue |
| `running` | Actively being worked on |
| `paused` | Waiting for your input (approval, budget increase, clarification) |
| `awaiting_approval` | Changes ready, needs your sign-off |
| `completed` | Successfully finished |
| `failed` | Could not complete (see diagnostics) |

## Cancelling a Task

Via API:
```bash
curl -X POST http://localhost:8080/v1/tasks/rate-limit-001/cancel
```

This checkpoints the current state and cleans up the worktree.

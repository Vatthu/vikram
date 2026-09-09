# Team Formations

A formation is a preset team configuration — which models handle which roles for a specific type of work.

## Why Formations?

Different tasks benefit from different team configurations:
- A **documentation task** doesn't need an expensive reviewer
- A **security audit** needs the most capable models everywhere
- A **quick bugfix** benefits from fast, cheap models
- A **complex refactor** needs strong planning and careful review

## Default Formations

Vikram comes with sensible defaults:

| Formation | Lead | Engineer | Reviewer |
|-----------|------|----------|----------|
| `bugfix` | Claude Sonnet | DeepSeek | GPT-4o |
| `feature` | Claude Sonnet | Claude Sonnet | GPT-4o |
| `refactor` | Claude Opus | Claude Sonnet | Claude Sonnet |
| `documentation` | GPT-4o-mini | GPT-4o-mini | — (auto-approve) |
| `security-audit` | Claude Opus | Claude Opus | Claude Opus |

## Creating a Custom Formation

Via API:
```bash
curl -X POST http://localhost:8080/v1/formations \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-fast-formation",
    "task_type": "bugfix",
    "role_mappings": {
      "lead": {"provider": "groq", "model": "llama-3.3-70b-versatile"},
      "engineer": {"provider": "deepseek", "model": "deepseek-chat"},
      "reviewer": {"provider": "openai", "model": "gpt-4o-mini"}
    },
    "budget_strategy": {
      "planning": 5,
      "implementation": 70,
      "verification": 10,
      "review": 15
    }
  }'
```

## Using a Formation

When submitting a task:
```json
{
  "task_id": "quick-fix-001",
  "objective": "Fix the typo in the error message",
  "repo": {"path": "/path/to/repo", "default_branch": "main"},
  "formation": "my-fast-formation"
}
```

## Automatic Formation Selection

If you don't specify a formation, Vikram selects one based on:
1. Task complexity assessment (file count, change type, security relevance)
2. Historical success rates for similar tasks
3. Budget position

## Formation Effectiveness

Vikram tracks how each formation performs:
- Success rate
- Average cost per task
- Average duration
- First-attempt verification pass rate

View via: Console → Formations tab, or `GET /v1/formations/effectiveness`

If a formation consistently underperforms (success rate below 60% over 20 tasks), you get a recommendation to adjust it.

## Complexity Tiers

Vikram classifies tasks into:

| Tier | Characteristics | Model selection |
|------|----------------|-----------------|
| Routine | Docs, config, single-line | Cheapest capable model |
| Moderate | Single-file logic, tests | Balanced model |
| Complex | Multi-file, new features | Strong model |
| Critical | Architecture, security | Best available model |

The formation can be overridden by complexity — if a "bugfix" formation is selected but the task is classified as "critical", models are upgraded automatically.

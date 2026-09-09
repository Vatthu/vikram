# Cost & Budget

Vikram tracks every dollar spent and gives you full control over limits.

## How Costs Are Tracked

Every LLM call records:
- Input tokens, output tokens
- Model and provider used
- Which task, role, and phase generated it
- Wall-clock duration
- Computed cost in USD

You can query this at any time via the Console cost dashboard or API.

## Setting Limits

### Per-task limit

In your task submission:
```json
{
  "constraints": {
    "max_cost_usd": 3.00
  }
}
```

What happens:
- At 80% ($2.40): you get a warning notification
- At 100% ($3.00): task pauses, asks you to increase or cancel

### Daily system limit

In settings:
```json
{
  "budget": {
    "daily_ceiling_usd": 20.00,
    "reset_hour_utc": 0
  }
}
```

When hit: ALL active tasks pause simultaneously. Resets at the configured hour.

## Budget Strategy

Instead of just capping spend, you can allocate it as a strategy:

```json
{
  "budget_strategy": {
    "planning": 10,
    "implementation": 60,
    "verification": 10,
    "review": 20
  }
}
```

This means: of a $5 task budget, $3 goes to implementation, $1 to review, $0.50 each to planning and verification.

**What this affects:**
- When the implementation phase runs out of its 60% share, Vikram switches to cheaper models for remaining calls in that phase
- The expensive reviewer model gets its full 20% allocation preserved

## Cost Forecasting

Before a task starts, Vikram estimates its cost based on:
- Complexity (file count, change type)
- Historical data from similar completed tasks

If the forecast exceeds your configured budget, you're notified before execution begins.

## Typical Costs

| Task type | Typical cost |
|-----------|-------------|
| Documentation changes | $0.10–$0.50 |
| Config/single-file fix | $0.50–$1.50 |
| Normal feature (3-5 files) | $1.50–$4.00 |
| Major refactoring | $3.00–$8.00 |

These vary significantly based on which models you use. DeepSeek is ~10x cheaper than Claude for implementation; GPT-4o is between them.

## Viewing Costs

- **Console**: Cost dashboard with breakdowns by task, role, model, and time
- **API**: `GET /v1/telemetry/cost?start_time=...&end_time=...`
- **Per task**: `GET /v1/cost/task/{task_id}`

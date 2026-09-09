# Approval & Governance

Vikram's governance system determines what needs your sign-off and what can proceed autonomously.

## The Approval Matrix

Create `.vikram/approval-matrix.yaml` in your repository:

```yaml
version: 1
rules:
  # Security files always need human review
  - name: security-always-review
    priority: 1
    conditions:
      file_patterns: ["**/auth/**", "**/security/**", "**/*secret*"]
    routing: founder_review

  # Documentation auto-approves after tests pass
  - name: docs-auto-approve
    priority: 10
    conditions:
      risk_level: [low]
      file_patterns: ["**/*.md", "docs/**"]
      min_confidence_score: 5
    routing: auto_approve

  # Small changes with high trust can auto-approve
  - name: trusted-small-changes
    priority: 20
    conditions:
      risk_level: [low, medium]
      max_files_changed: 3
      max_lines_changed: 50
      min_confidence_score: 15
    routing: auto_approve

  # Everything else needs review
  - name: default
    priority: 999
    conditions: {}
    routing: founder_review
```

### How rules are evaluated

1. Rules are checked in **priority order** (lowest number first)
2. The **first rule whose conditions all match** determines the routing
3. If no rule matches, the default is `founder_review`

### Available conditions

| Condition | Type | Meaning |
|-----------|------|---------|
| `risk_level` | list of strings | Match if change risk is in this list |
| `file_patterns` | list of globs | Match if any changed file matches |
| `max_files_changed` | integer | Match if total files changed ≤ this |
| `max_lines_changed` | integer | Match if total lines changed ≤ this |
| `min_confidence_score` | float | Match if confidence score ≥ this |
| `max_cost_consumed_pct` | float | Match if cost consumed ≤ this % of budget |
| `repo_path_prefix` | list of strings | Match if repo path starts with any of these |

### Routing outcomes

| Routing | Effect |
|---------|--------|
| `auto_approve` | Task proceeds to merge gate without asking you |
| `founder_review` | You get a notification and must approve/reject |
| `escalate_and_halt` | Task pauses immediately, strong notification sent |

## Risk Classification

Vikram automatically classifies every change by risk level:

| Level | When |
|-------|------|
| **low** | Documentation-only, config comments, README changes |
| **medium** | Single-file logic changes, test additions |
| **high** | Multi-file code changes, dependency updates |
| **critical** | Security-related files, infrastructure, authentication code |

You can define custom risk patterns in `.vikram/risk-patterns.yaml`:

```yaml
patterns:
  - path: "**/auth/**"
    risk: critical
  - path: "**/migrations/**"
    risk: high
  - path: "**/*.test.*"
    risk: low
```

## Confidence Score

Vikram earns trust over time through successful completions.

**How it works:**
- Each successful task (verified, approved, no issues for 48h): **+1 point**
- Each failure (rollback, reported issue): **-3 points**

**What it unlocks:**
- At 10 points (routine tier): documentation changes can auto-approve
- At 20 points (moderate tier): small code changes can auto-approve
- At 50 points (complex tier): multi-file changes can auto-approve
- Critical tier: **never auto-promotes** regardless of score

**Check your scores:**
- Console → Team Health shows current confidence per tier
- Or query: `GET /v1/approvals/audit`

**Reset manually:**
If you lose trust in the system, reset confidence scores from the Console.

## Approval Flow (What You See)

When a task needs your approval:

1. **Telegram** (if configured): You get a message with:
   - Task objective
   - Files changed with line counts
   - Risk level
   - Verification results (pass/fail)
   
2. **Console**: The task shows in the approval queue with full diff viewer

3. **Your options:**
   - **Approve** — merge proceeds
   - **Reject** — worktree is rolled back, task marked failed
   - **Edit and approve** — tell Vikram what to change, it revises
   - **Clarify** — ask a question before deciding

## Hot-Reload

Update your approval matrix without restarting:
- Edit `.vikram/approval-matrix.yaml` in your repo
- Vikram detects the change within 5 seconds
- New rules apply to the next decision point
- The change is recorded in the execution trace

## Audit Trail

Every approval decision is permanently logged with:
- The exact state at decision time
- Which rule matched
- All inputs (risk level, file patterns, scope, confidence, cost)
- The routing outcome
- Any founder override and reason

Query via: `GET /v1/approvals/audit?task_id=...`

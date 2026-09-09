# Understanding the Workflow

When you submit a task, Vikram runs through a structured engineering workflow. Here's what happens at each phase.

## The Phases

```
Task Submitted
     │
     ▼
┌─────────────┐
│  Planning   │  Lead agent analyzes the repo and creates an implementation plan
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Grilling   │  A "Devil's Advocate" attacks the plan (up to 3 rounds)
└──────┬──────┘
       │
       ▼
┌─────────────┐
│Implementation│  Engineer agent writes code in an isolated git branch
└──────┬──────┘
       │
       ▼
┌─────────────┐
│Verification │  Runs: linting → tests → property checks → independent review
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Approval   │  Auto-approves low-risk OR asks you for sign-off
└──────┬──────┘
       │
       ▼
┌─────────────┐
│ Merge Ready │  Branch is clean, verified, reviewed, and ready for you
└─────────────┘
```

## Phase Details

### 1. Planning

The lead agent:
- Reads your repository structure (key files, dependencies, build config)
- Identifies which files need to change
- Creates a step-by-step implementation plan
- Estimates risk level

### 2. Adversarial Grilling

A separate model plays "Devil's Advocate" and tries to find flaws in the plan:
- Missing edge cases
- Potential regressions
- Incorrect assumptions about the codebase

The lead revises the plan based on criticism. This loops up to 3 times. If the critic says "CONCEDE" — the plan passes.

### 3. Implementation

The engineer agent:
- Creates a git worktree (isolated copy of your repo on a new branch)
- Writes the code changes as bounded text replacements (old_text → new_text)
- Each edit is atomic and auditable

If the engineer's output can't be parsed (malformed JSON), Vikram retries with a corrective prompt. If that fails, the task halts.

### 4. Verification

Four-layer verification:

1. **Lint guard** — runs your linter, checks for new errors introduced by the changes
2. **Test execution** — runs your project's test command
3. **Property verification** — generates and runs formal correctness properties (optional, based on change type)
4. **Independent review** — a different model from the engineer evaluates the diff

### 5. Approval

The approval matrix evaluates:
- Risk level of the change (low/medium/high/critical)
- File patterns matched (security files always require review)
- Number of files/lines changed
- Your configured confidence score for this complexity tier

Result: `auto_approve`, `founder_review`, or `escalate_and_halt`

### 6. Merge Ready

When approved, Vikram evaluates the merge gate:
- Branch is up-to-date with main
- All verification passed
- No file conflicts with other active tasks
- Review approved

You can then merge from the console, Telegram, or CLI.

## How Changes Are Made

Vikram never uses `git commit` with arbitrary diffs. Every edit is a **bounded text replacement**:

```json
{
  "path": "src/handlers/user.go",
  "old_text": "func CreateUser(w http.ResponseWriter, r *http.Request) {",
  "new_text": "func CreateUser(w http.ResponseWriter, r *http.Request) {\n\tif err := validateInput(r); err != nil {\n\t\thttp.Error(w, err.Error(), 400)\n\t\treturn\n\t}"
}
```

This means:
- Every change is auditable (you see exactly what was replaced)
- Changes are atomic (if any edit fails, the worktree is rolled back)
- No "phantom changes" — nothing happens that isn't explicitly in the edit list

## Isolation Model

Each task gets its own:
- **Git worktree** — separate directory, separate branch, separate HEAD
- **Session state** — conversation history, checkpoints, artifacts
- **Budget tracking** — cost is per-task
- **Lock registry** — files being edited are locked against other concurrent tasks

Your main branch is never touched. The worktree lives at `~/.vikram/worktrees/{task_id}/`.

## What Gets Produced

Each task produces these **artifacts** (stored and queryable):

| Artifact | Contents |
|----------|----------|
| Plan | The implementation approach |
| Implementation | The actual edits applied |
| Verification | Test results, lint output, property results |
| Review | Independent model's verdict and issues found |
| Approval | Policy evaluation result |
| Merge Assessment | Whether the branch is safe to merge |

All artifacts are accessible via the Console or API.

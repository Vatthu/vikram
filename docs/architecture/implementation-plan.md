# Implementation Plan

This is Vikram's current implementation plan.

It is intentionally flexible. The target architecture is stable, but the sequence can change when research or implementation exposes a better path.

## Planning Rule

- keep the destination stable
- keep interfaces explicit
- keep sequencing adaptive
- promote a pattern into product code only after it survives research and a concrete use case

## Current Build Order

### Phase 0: Research and source hygiene

Goal:

- verify upstream sources
- extract high-signal patterns
- reject contaminated or low-trust inputs

Status:

- in progress, but good enough to unblock implementation

Exit criteria:

- upstream manifest is real
- extraction notes exist
- source policy is explicit

### Phase 1: Contract-first foundation

Goal:

- define the shared Go/Python task contract
- make task, artifact, approval, action, and observation objects explicit
- align docs and code to the same names

Why this phase exists:

Without a typed contract, the architecture will drift and research findings will stay trapped in prose.

Exit criteria:

- shared contract models exist in Go and Python
- Go/Python contract doc references real type names
- orchestrator service stores and returns the typed session model

### Phase 2: Host daemon capability layer

Goal:

- carve the Go side into a real host-executor surface
- expose workspace, git, file, shell, notify, and health capabilities through internal APIs

Exit criteria:

- internal executor endpoints exist for the first action set
- permissions and audit rules are enforced in Go
- Python can request host actions without directly touching the machine

### Phase 3: Orchestrator workflow v1

Goal:

- implement the first end-to-end LangGraph workflow for one repo and one task
- use artifacts instead of free chat between phases

Exit criteria:

- intake to plan to implement to verify to review runs as one resumable workflow
- founder approval can pause and resume the workflow
- task state survives process restart

### Phase 4: Founder operations layer

Goal:

- connect Telegram approvals, blocker notifications, and phase summaries
- make Vikram usable as an operator system, not only as code

Exit criteria:

- approval requests are delivered through Telegram
- decisions resume the workflow
- phase-complete summaries are founder-readable

### Phase 5: Verification hardening

Goal:

- add richer verification, browser checks, and review policy
- improve trust before adding more autonomy

Exit criteria:

- verification artifacts are consistent
- review and verification are separated
- browser and visual checks are available when needed

## Adaptive Rules

The plan can change, but only under these rules:

- do not skip the typed contract
- do not add more agents before the first workflow is reliable
- do not widen tool access before audit and approval rules exist
- do not add self-optimizing workflow logic before evaluation artifacts exist

## What Changes The Plan

Valid reasons to reorder or refine steps:

- research reveals a better boundary
- implementation exposes a missing prerequisite
- operator usability shows a workflow gap
- cost, reliability, or trust concerns force a narrower v1

Invalid reasons to change direction:

- novelty for its own sake
- adding more frameworks without a concrete integration target
- chasing benchmark features that do not improve founder trust

## Current State

Completed enough to move forward:

- Phase 1 contract types exist in Go and Python
- the Go host daemon now exposes `health`, `workspace provision`, `exec`, and `telegram notify` over a Unix socket
- the Go host daemon now exposes native `git worktree create` and `artifact write` capabilities
- the Go host daemon now exposes native `git worktree remove` and repo inspection capabilities
- the Go host daemon now exposes native target discovery and bounded file-read capabilities
- the Go host daemon now exposes native bounded file-write and verification-discovery capabilities
- the Go host daemon now exposes native exact text replacement for bounded in-file edits
- the Python orchestrator intake flow now verifies host health, provisions a task workspace, creates a task worktree, inspects repository state, localizes likely change targets, reads bounded file previews, and writes plan, implementation, and verification-preparation artifacts through that contract
- the Python orchestrator can now resume a saved thread with a bounded change request, apply exact replacements, run focused verification, and persist change and verification-result artifacts
- the Python orchestrator now evaluates each change attempt against an explicit approval matrix, auto-completes low-risk verified documentation changes, and pauses risky or failed attempts for founder review
- the Python orchestrator can notify Telegram when an operator route is configured, and can resume the saved thread from a founder decision
- onboarding and channel configuration now support an env-backed Telegram token path instead of only visible raw-config input
- the Python orchestrator now exposes founder-console list and review-detail endpoints for task inspection
- founder `edit_and_approve` and `clarify` decisions now create native follow-up context that carries into the next bounded change attempt
- a first thin founder console is now served by the orchestrator as static HTML/CSS/JS on top of those endpoints
- the founder console now renders structured review evidence: bounded edit diffs, verification runs, and artifact previews
- approval is now separated from merge readiness; approved changes continue through post-change repo inspection and a dedicated merge-readiness assessment
- the Python orchestrator now writes merge-readiness artifacts and finalizes approved attempts as `merge_ready` or `merge_blocked`
- the founder console now surfaces merge-readiness state alongside approval and verification evidence
- the Go host daemon now exposes native artifact reads for founder-review drill-down
- the Python orchestrator now exposes bounded artifact-content fetches for review-approved task artifacts
- the founder console now supports full artifact drill-down instead of only file paths and previews
- repo inspection now returns structured merge handoff evidence: branch, HEAD, changed-file count, additions, deletions, short diff stat, and per-file summaries
- merge readiness now uses changed-file evidence and expected task branch checks instead of relying only on raw git status lines
- the founder console now shows branch, HEAD, diff stat, and changed files inside merge-readiness review
- the surviving local `vikram_agent` workspace material was restored into `archive/prototypes/vikram_agent` as historical context
- `merge_blocked` now acts as a retryable paused state; merge blockers are carried into the next bounded change attempt as follow-up context

## Current Next Step

The active next step is repository protection and merge handoff hardening:

- exercise the stronger merge handoff evidence on real task flows
- refine retry policy after founder follow-up once the current merge gate is exercised on real tasks
- widen notification routing and approval UX only after the current founder loop is stable

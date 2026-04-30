# LeVik Orchestrator

This service is the Python workflow brain for LeVik.

## Responsibilities

- accept tasks from the Go host daemon
- manage workflow state with LangGraph
- apply capability-based model routing
- produce artifacts for plan, implementation, testing, and review
- pause and resume on human approval

## Non-Responsibilities

- direct shell execution on the host
- direct file system mutations outside the host contract
- secret storage
- Telegram delivery

Those stay in the Go host layer.

## Runtime

The service is designed to run locally over a Unix domain socket.

The first implementation is intentionally narrow:

- health endpoint
- create task endpoint
- list task endpoint
- apply bounded change endpoint
- inspect task endpoint
- review detail endpoint
- review artifact content endpoint
- a LangGraph intake workflow that verifies the host daemon and provisions a task workspace

## Current Workflow

The current task path is:

- intake
- host health verification
- workspace provisioning
- git worktree creation
- repo inspection
- target discovery
- bounded target file reads
- plan artifact persistence
- implementation brief persistence
- verification discovery
- verification plan persistence
- task session persistence

This is now a localized, repo-aware `change_ready` flow. It prepares edits and focused verification and waits for an explicit bounded change request before mutating the worktree.

The second-stage execution path is now available through `POST /v1/tasks/{task_id}/changes`:

- resume the saved LangGraph thread for that task
- apply exact text replacements through the Go host contract
- run the selected verification command through the host executor
- persist implementation and verification-result artifacts for that attempt

This keeps edit execution explicit and bounded. It still does not hardcode any single model into the orchestrator as the patch generator.

Founder control now sits on top of that path through an explicit approval matrix:

- low-risk single-file documentation changes can auto-complete after successful verification
- multi-file, code, configuration, automation, or failed-verification attempts are risk-classed and escalated
- LeVik writes an approval artifact before pausing
- LeVik can notify Telegram when `operator_channel` and `operator_chat_id` are provided
- `POST /v1/tasks/{task_id}/resume` resumes the saved LangGraph thread with an approval decision
- approval is not the final state anymore; approved attempts continue into post-change repo inspection and merge-readiness evaluation

Current output semantics:

- `risk_class` and `approval_route` are returned on the task session once a change attempt has been evaluated
- `requires_founder_review` is true only when the approval route is `founder_review`
- successful low-risk attempts pass through `auto_approved` and then finalize as `merge_ready` or `merge_blocked`
- founder-approved attempts also finalize as `merge_ready` or `merge_blocked`
- `merge_readiness` and `merge_summary` are returned on the task session once post-change assessment finishes

Founder-console backend surface now exists for the next UI layer:

- `GET /v1/tasks`
  - list tasks with optional filters for `status`, `phase`, `needs_review`, and `follow_up_required`
  - `needs_review=true` now means the task is currently `awaiting_approval`, not merely that it once required founder review
- `GET /v1/tasks/{task_id}/review`
  - return review-state detail including approval payload, founder decision, merge assessment, applied edit evidence, verification runs, and artifact paths
- `GET /v1/tasks/{task_id}/artifacts/content`
  - return bounded full content for a review-approved artifact path tied to that task

The first founder console is now served directly by the orchestrator at `/console`:

- static HTML/CSS/JS
- same-origin calls to the review and decision endpoints
- intentionally thin so a later proper frontend can replace it without changing workflow state or approval semantics
- review cards now include bounded change evidence, verification runs, merge-readiness assessment, and artifact previews instead of only status labels
- artifact drill-down is now native: the console can load full content for the current task’s review artifacts without bypassing the host contract

Follow-up handling is now native too:

- `edit_and_approve` and `clarify` decisions create a pending follow-up context
- the next bounded change attempt carries that founder feedback into its change artifact
- once a new attempt starts, the pending follow-up is cleared from the task session

Merge readiness now sits after approval:

- LeVik re-inspects repo state after an approved or auto-approved attempt
- LeVik writes a merge-readiness artifact with blockers, notes, and current git status
- `merge_ready` means the change has bounded edits, passing verification, detected changed files on the expected task branch, and no open follow-up
- `merge_blocked` means the change is approved or executed but still not safe to hand off as merge-ready
- merge handoff evidence now includes branch, HEAD, changed-file count, additions, deletions, short diff stat, and changed-file summaries
- `merge_blocked` tasks are retryable; their blockers become follow-up context for the next bounded change attempt

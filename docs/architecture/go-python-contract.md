# Go-Python Contract

Current code anchors:

- Go: `pkg/orchestrator/types.go`
- Go host server: `pkg/orchestratorhost/server.go`
- Python: `services/orchestrator/src/vikram_orchestrator/models.py`
- Python client: `services/orchestrator/src/vikram_orchestrator/host_client.py`

## Transport

The Go host daemon and Python orchestrator communicate over local HTTP+JSON on a Unix domain socket.

Reason:

- simpler than inventing a new RPC layer
- easy to debug with standard tooling
- no public TCP port needed
- compatible with Go and Python without extra protocol complexity

## Ownership Rule

Go is the only side allowed to perform host actions directly.

Python is the only side allowed to decide workflow state transitions.

## Direction A: Go To Python

Go sends requests to Python for orchestration actions.

Primary shared object:

- `TaskSession`

Initial endpoints:

- `POST /v1/tasks`
  - create a new orchestrated task
- `GET /v1/tasks`
  - list orchestrated tasks for founder-console views
- `POST /v1/tasks/{task_id}/changes`
  - apply a bounded change proposal and run focused verification
- `POST /v1/tasks/{task_id}/resume`
  - resume a paused task with human input
- `GET /v1/tasks/{task_id}`
  - fetch current task state
- `GET /v1/tasks/{task_id}/review`
  - fetch review-state detail for founder inspection
- `GET /v1/tasks/{task_id}/artifacts/content`
  - fetch bounded full content for a review-approved artifact path
- `GET /console`
  - serve the thin founder-console UI
- `GET /healthz`
  - orchestrator health

Example task payload:

```json
{
  "task_id": "task_001",
  "source": "telegram",
  "requested_by": "founder",
  "objective": "Implement OAuth callback hardening",
  "repo": {
    "path": "/Users/vikram/repos/app",
    "default_branch": "main"
  },
  "operator_channel": "telegram",
  "operator_chat_id": "123456789",
  "constraints": {
    "require_human_approval": true,
    "max_parallel_workers": 1
  }
}
```

## Direction B: Python To Go

Python calls Go for host capabilities.

Primary shared objects:

- `HostActionSpec`
- `HostActionRequest`
- `HostObservation`
- `Artifact`
- `ApprovalRequest`
- `ApprovalDecision`

Initial endpoints:

- `POST /v1/workspaces/provision`
- `POST /v1/workspaces/cleanup`
- `POST /v1/git/worktrees/create`
- `POST /v1/git/worktrees/remove`
- `POST /v1/exec`
- `POST /v1/files/read`
- `POST /v1/files/write`
- `POST /v1/notify/telegram`
- `GET /v1/system/health`

Implemented first-pass endpoints:

- `GET /healthz`
- `GET /v1/system/health`
- `POST /v1/workspaces/provision`
- `POST /v1/git/worktrees/create`
- `POST /v1/git/worktrees/remove`
- `POST /v1/repos/inspect`
- `POST /v1/repos/discover-targets`
- `POST /v1/repos/discover-verification`
- `POST /v1/files/read`
- `POST /v1/files/write`
- `POST /v1/files/replace`
- `POST /v1/artifacts/write`
- `POST /v1/artifacts/read`
- `POST /v1/exec`
- `POST /v1/notify/telegram`

Current verified workflow usage:

- Python task intake calls `GET /v1/system/health`
- Python then calls `POST /v1/workspaces/provision`
- Python then calls `POST /v1/git/worktrees/create`
- Python then calls `POST /v1/repos/inspect`
- Python then calls `POST /v1/repos/discover-targets`
- Python then calls `POST /v1/files/read` on the top candidates
- Python then writes a plan artifact
- Python then writes an implementation brief artifact
- Python then calls `POST /v1/repos/discover-verification`
- Python then writes a verification plan artifact
- the resulting `TaskSession` is stored with the `change_ready` phase
- `GET /v1/tasks` can now list those sessions for founder-console use
- a later `POST /v1/tasks/{task_id}/changes` call injects a bounded change request into the saved LangGraph thread
- Python then calls `POST /v1/files/replace` for exact text replacements inside the managed worktree
- Python then calls `POST /v1/exec` for the selected verification command
- Python then writes implementation and verification result artifacts for that change attempt
- Python then evaluates an explicit approval matrix:
  - low-risk documentation-only changes can auto-complete after successful verification
  - multi-file, code, configuration, automation, or failed-verification changes are escalated or stopped according to task constraints
- if founder review is required, Python writes an approval artifact, optionally sends a Telegram notification, and pauses the LangGraph thread awaiting a founder decision
- `GET /v1/tasks/{task_id}/review` exposes the current approval payload, artifact paths, founder decision, and pending follow-up context
- `edit_and_approve` and `clarify` decisions create follow-up context that is attached to the next bounded change attempt
- `POST /v1/tasks/{task_id}/resume` resumes that paused thread with an `ApprovalDecision`
- once a change is auto-approved or founder-approved, Python re-inspects repo state, evaluates merge readiness, and writes a merge-readiness artifact
- approved changes now finalize as either `merge_ready` or `merge_blocked`
- `GET /v1/tasks/{task_id}/artifacts/content` now proxies only the current task's review-approved artifact paths through `POST /v1/artifacts/read`
- post-change repo inspection now includes branch, HEAD, changed-file count, additions, deletions, short diff stat, status lines, and changed-file summaries
- if a task finalizes as `merge_blocked`, the next `POST /v1/tasks/{task_id}/changes` carries the merge blockers into the new change artifact as retry context

## Shared Object Families

- `TaskSession`
  - task identity, repo, constraints, status, phase, summary, risk class, approval route, follow-up state, and merge readiness state
- `HostActionSpec`
  - declarative description of a host capability
- `HostActionRequest`
  - one execution request issued by Python
- `HostObservation`
  - normalized action result returned by Go
- `Artifact`
  - structured workflow output stored outside chat history
- `ArtifactReadRequest`
  - bounded read request for a persisted review artifact
- `RepoChangedFile`
  - bounded per-file merge handoff evidence with path, status, additions, deletions, and binary flag
- `ApprovalRequest`
  - founder-facing decision payload with risk class, reasons, and related artifacts
- `ApprovalDecision`
  - founder decision used to resume a paused task
- `TaskReviewDetail`
  - founder-console detail view built from the saved task session and LangGraph thread state
  - includes approval payload, follow-up state, merge assessment, applied-edit evidence, verification runs, and artifact previews

## Approval Model

Python requests approval through Go by returning a paused state.

Go delivers the approval request to Telegram and later resumes the task with:

- approve
- reject
- edit-and-approve
- clarify

When founder review is not required, Python continues through merge-readiness assessment and returns a task session with:

- `risk_class`
- `approval_route`
- `requires_founder_review`
- `merge_readiness`
- `merge_summary`

Founder approval does not imply merge readiness by itself.

After auto-approval or founder approval, Python:

- inspects post-change repo state
- checks for passing verification, detected changed files, expected task branch, and unresolved follow-up
- writes a merge-readiness artifact
- finalizes the task as either `phase=merge_ready, status=completed` or `phase=merge_blocked, status=paused`

`merge_blocked` is a retryable paused state, not a terminal failure.
The founder console marks it as follow-up capable, and the next bounded change attempt receives the merge blockers and notes as active follow-up context.

## Artifact Model

Python produces artifacts. Go stores and transports them.

Artifact types:

- task spec
- plan
- implementation
- verification
- review
- founder approval
- blocker

Current review artifacts include:

- approval request
- founder decision
- merge readiness

## Versioning

The internal contract is versioned under `/v1/`.

Breaking changes require:

- a doc update in this file
- a matching update on both sides
- a migration note in the roadmap or release notes

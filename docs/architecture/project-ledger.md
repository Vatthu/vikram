# LeVik Project Ledger

Last updated: 2026-04-28

This is the founder-facing status ledger for LeVik.

It exists to answer four questions in one place:

1. What is already completed?
2. What is currently active?
3. What still needs to be built?
4. Which research source changed which part of LeVik?

This file is the compressed operational view.
The deeper detail still lives in `implementation-plan.md`, `go-python-contract.md`, `levik-v1-synthesis.md`, and `research/notes/`.

## Current Shape

LeVik v1 is currently being built as:

- a Go host daemon
- a Python orchestrator
- a typed contract between them
- a founder-operated engineering system
- a host-native system, not a Docker-in-Docker control plane

## Completed

### 1. Mission and repo direction

Completed:

- LeVik was reframed away from a Jarvis-style assistant and toward a founder-operated engineering team
- architecture docs were created for host-native deployment, repo layout, model policy, contract boundaries, and implementation sequencing

Primary docs:

- `README.md`
- `ROADMAP.md`
- `docs/architecture/`

### 2. Research foundation

Completed:

- upstream study area created under `research/upstream/`
- real clone manifest created and pinned
- source provenance policy written
- first-pass and extraction notes written for the main frameworks and papers

Primary docs:

- `research/upstream/MANIFEST.md`
- `research/STUDY_LIST.md`
- `research/SOURCE_POLICY.md`
- `research/notes/`

### 3. Core architecture boundary

Completed:

- stable split chosen: `Go host daemon + Python orchestrator + LangGraph core`
- model assignment kept dynamic instead of hardcoded to specific vendors
- artifact-first workflow chosen over chat-heavy agent loops

Primary docs:

- `docs/architecture/levik-v1-synthesis.md`
- `docs/architecture/model-policy.md`
- `docs/architecture/go-python-contract.md`

### 4. Typed contract

Completed:

- shared task and host models now exist in Go and Python
- the contract covers task sessions, host actions, artifacts, approvals, and review state

Primary code:

- `pkg/orchestrator/types.go`
- `services/orchestrator/src/levik_orchestrator/models.py`

### 5. Go host daemon capability layer

Completed:

- system health
- workspace provisioning
- git worktree create/remove
- repo inspection
- target discovery
- verification discovery
- bounded file reads
- bounded file writes
- bounded text replacement
- artifact write
- artifact read
- focused command execution
- Telegram notification

Primary code:

- `pkg/orchestratorhost/server.go`

### 6. Python orchestrator workflow v1

Completed:

- intake
- host health verification
- workspace provisioning
- worktree creation
- repo inspection
- target localization
- bounded target file reads
- plan artifact
- implementation brief artifact
- verification plan artifact
- change-ready session persistence
- bounded change execution
- focused verification execution
- approval matrix evaluation
- founder pause/resume flow
- follow-up edit handling
- post-approval merge-readiness evaluation

Primary code:

- `services/orchestrator/src/levik_orchestrator/workflow.py`
- `services/orchestrator/src/levik_orchestrator/server.py`

### 7. Founder operations layer

Completed:

- task list endpoint
- review detail endpoint
- founder decision endpoint
- artifact content endpoint for review-approved artifacts
- first thin founder console
- review evidence rendering
- merge-readiness rendering
- artifact drill-down in the console

Primary code:

- `services/orchestrator/src/levik_orchestrator/server.py`
- `services/orchestrator/src/levik_orchestrator/console/`

### 8. Verification quality improvements

Completed:

- merge readiness is now separate from approval
- low-risk auto-approved changes no longer skip merge evaluation
- founder-approved changes can still be merge-blocked
- sqlite checkpointer cleanup was tightened so Python tests run cleanly
- merge handoff evidence now includes branch, HEAD, changed-file count, additions, deletions, short diff stat, and changed-file summaries
- the surviving local `vikram_agent` workspace material was restored into `archive/prototypes/vikram_agent` for historical context
- `merge_blocked` is now retryable, and its blockers are carried into the next bounded change attempt as follow-up context
- local git was initialized and baseline commit `2a2c726` was created for recovery protection

Primary code:

- `services/orchestrator/src/levik_orchestrator/workflow.py`
- `services/orchestrator/src/levik_orchestrator/server.py`

## Current Status

LeVik currently has:

- the correct high-level architecture
- a real host-native workflow
- a real founder review loop
- a real review UI
- a real approval-to-merge boundary
- a build-clean uncommitted candidate patch from the 2026-04-27 assistant handoff after Codex review fixes

LeVik does not yet have:

- a committed acceptance point for the full 2026-04-27 patch set
- browser/visual verification through a constrained runner
- email archival summaries
- richer audit/event reporting
- cost-aware model routing in execution
- multi-role parallel execution beyond the current bounded loop

## 2026-04-28 Trust Boundary Review

The previous trusted git commit is `1202b5e Add merge-blocked retry context`.

The large 2026-04-27 assistant patch remains uncommitted. Codex reviewed it as untrusted, fixed build and safety blockers, and verified:

- `go test ./...`
- `python -m unittest discover -s services/orchestrator/tests`
- `python3 -m py_compile services/orchestrator/src/levik_orchestrator/*.py`
- `git diff --check`

Corrections made during review:

- rollback no longer relies on a nonfunctional pre-edit stash; managed task worktrees reset and clean to `HEAD`
- generated Playwright/Node browser-test execution is disabled until a constrained runner exists
- management console and dashboard are opt-in local services instead of always-on network surfaces
- GitHub Copilot is no longer advertised as a supported selectable provider
- MCP client now sends `notifications/initialized` and handles larger JSON-RPC messages
- web build artifacts and `node_modules` are ignored

## Active Next Steps

The next build targets are:

### Near term

- exercise the stronger merge handoff evidence on real task flows
- refine retry policy after founder follow-up on more real tasks

### After that

- add browser and visual verification when needed
- add email archival summaries after approved milestones
- widen the founder console from thin review UI into a stronger control panel

### Later

- deeper model routing and budget policy
- typed task/event telemetry
- controlled parallel specialists under the main workflow

## Research To Product Mapping

### LangGraph

What it gave:

- durable execution
- checkpoints
- pause/resume with interrupts
- graph-structured workflow control

What changed in LeVik:

- Python orchestrator is built around a resumable workflow graph
- founder approval pauses the real workflow instead of being simulated in chat

Status:

- adopted directly as a dependency

### SWE-agent

What it gave:

- agent-computer interface discipline
- purpose-built repo tools
- post-action state thinking
- observation shaping

What changed in LeVik:

- typed host contract
- bounded repo operations
- localization before editing
- explicit artifact and verification flow

Status:

- adopted as design doctrine, not copied runtime

### MetaGPT

What it gave:

- explicit roles
- SOP thinking
- artifact-driven handoff

What changed in LeVik:

- stable logical roles remain part of the architecture
- artifacts, not agent chatter, are the main workflow boundary

Status:

- selectively adopted

### OpenHands SDK

What it gave:

- task/session as a first-class runtime object
- workspace boundary discipline
- event-oriented thinking
- orchestrator vs execution separation

What changed in LeVik:

- `TaskSession` is first-class
- Go owns execution, Python owns workflow intelligence

Status:

- selectively adopted

### OpenClaw

What it gave:

- operator and channel ergonomics
- Telegram/control-plane ideas
- multi-agent routing lessons

What changed in LeVik:

- reinforced founder-first approval and notification design
- influenced operator-surface thinking, not the core runtime

Status:

- used as a reference, not a base

### Agentless, AgentFL, LingmaAgent, Repository Memory

What they gave:

- localization before repair
- repo-aware exploration
- stronger comprehension before mutation
- future direction for memory beyond chat history

What changed in LeVik:

- repo inspection and target localization now happen before code mutation
- merge and review artifacts are tied to repo observations, not only prompts

Status:

- partially adopted, with memory work deferred

### ChatDev

What it gave:

- communication protocol matters
- dehallucination and coordination rules matter

What changed in LeVik:

- reinforced the decision to reject free-form chat swarms

Status:

- lesson adopted, architecture rejected

### AFlow

What it gave:

- workflow design itself can become an optimization target

What changed in LeVik:

- not used in v1
- informs later evaluation and workflow-search direction

Status:

- deferred

## Where To Look For Detail

If you want the compressed operational view:

- `docs/architecture/project-ledger.md`

If you want the current build sequence:

- `docs/architecture/implementation-plan.md`

If you want the current system boundary:

- `docs/architecture/go-python-contract.md`

If you want the design synthesis:

- `docs/architecture/levik-v1-synthesis.md`

If you want the research evidence:

- `research/notes/`
- `research/STUDY_LIST.md`
- `research/upstream/MANIFEST.md`

## Update Rule

Whenever LeVik changes in a way that affects founder operations, architecture, or research adoption:

- update this ledger
- update the implementation plan if sequencing changed
- update the contract doc if the interface changed
- add or amend a research note if the change came from new upstream or paper research

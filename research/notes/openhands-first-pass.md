# OpenHands First Pass

## Source Snapshot

- local checkout: `research/upstream/OpenHands`
- origin: `https://github.com/All-Hands-AI/OpenHands`
- pinned `HEAD`: `fb98faf4acc8afb2df65db06053328c4dc43a637`
- official docs reviewed: SDK architecture and agent-server docs

## What The Repo Actually Is

`OpenHands` is no longer just a single app. The current upstream positions the Software Agent SDK as the foundation, with CLI, local GUI, cloud, and enterprise layers around it. The local repo shows:

- `openhands/` core runtime and controller code
- `openhands/architecture/` documentation for startup, execution, and observability
- `frontend/` and `enterprise/` product layers

The local `openhands/README.md` describes the core event model clearly: agent, controller, event stream, runtime, and session.

## What Problem It Solves Well

- coding-agent runtime boundaries
- event-driven execution loop
- conversation and session lifecycle
- workspace abstraction and isolation

Official docs reinforce an important architectural split: the same agent code can run locally or against remote workspaces by swapping workspace types, while the agent server handles HTTP, WebSocket streaming, and workspace operations.

## Why It Matters For Vikram

This is highly relevant to Vikram’s execution side even if Vikram stays host-native:

- keep the orchestrator separate from the execution environment
- standardize actions and observations
- treat sessions as task containers
- isolate workspace concerns behind a boundary

## First Extraction Decision

- borrow the action/observation and workspace-boundary ideas
- adapt the agent-server concepts into Vikram’s Go host executor API
- reject Docker-first assumptions as Vikram’s primary control-plane design

Vikram does not need to become OpenHands, but it should copy the discipline of separating orchestration from runtime and normalizing host actions behind a stable interface.

## Immediate Cautions

- A lot of OpenHands value is tied to Docker or remote runtime assumptions.
- The repo is broader than what Vikram needs for v1.
- Enterprise and UI layers are not where the architectural signal is highest for us.

## Next Inspection Targets

1. `openhands/controller/`
2. `openhands/events/`
3. `openhands/runtime` or equivalent runtime modules
4. `openhands/architecture/agent-execution.md`
5. `openhands/architecture/conversation-startup.md`

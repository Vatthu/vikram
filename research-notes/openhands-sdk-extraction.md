# OpenHands SDK Extraction

## Source Slice

- `software-agent-sdk/README.md`
- `openhands-sdk/openhands/sdk/conversation/conversation.py`
- `openhands-sdk/openhands/sdk/agent/agent.py`
- `openhands-sdk/openhands/sdk/event/base.py`
- `openhands-workspace/openhands/workspace/remote_api/workspace.py`
- `openhands-agent-server/openhands/agent_server/README.md`

## High-Signal Patterns

### 1. Conversation is the task container

The SDK treats `Conversation` as the unit that owns:

- an agent
- a workspace
- callbacks
- persistence behavior
- iteration controls

This is a strong pattern for Vikram. A task/session should be a first-class runtime object, not just an ad hoc request.

### 2. Workspace is a swappable boundary

The factory creates a local or remote conversation based on workspace type, while keeping the outer API stable.

That is exactly the right long-term shape for Vikram:

- host-native local execution first
- remote execution later without rewriting orchestrator logic

### 3. Event types are explicit and immutable

The SDK event model uses typed events with source, timestamp, and conversion rules for LLM history. This is higher quality than dumping arbitrary logs into a chat transcript.

### 4. Agent server is a transport and execution surface, not the whole brain

The agent server exposes REST and WebSocket APIs, stores events, and manages workspace-facing operations. This separation is useful even though Vikram will not copy the whole server design.

## What Vikram Should Borrow

- task/session as a first-class object
- workspace abstraction as a stable boundary
- typed events rather than raw chat logs
- separation between orchestrator and execution transport

## What Vikram Should Adapt

- Replace the remote agent server with a Go host daemon plus Unix socket for v1.
- Keep the idea of local-versus-remote workspace swapability in the interface design.
- Use typed events for task lifecycle and observability, but keep them minimal.

## What Vikram Should Reject

- depending on OpenHands transport or server packaging as Vikram’s foundation
- Docker-first assumptions as the default control-plane model
- copying deprecated V0 controller code from the OpenHands monorepo

## Concrete Vikram Impact

Vikram should define:

- `TaskSession`
- `WorkspaceHandle`
- `ActionEvent`
- `ObservationEvent`
- `ArtifactEvent`
- `ApprovalEvent`
- `TaskStatusEvent`

The Python orchestrator should own workflow state.
The Go daemon should own execution and transport.
The session/event model should connect them.

# LeVik V1 Synthesis

This document turns the upstream research into concrete design constraints for LeVik v1.

## Inputs We Are Actually Using

- `LangGraph`: durable workflow execution, checkpoints, interrupts
- `SWE-agent`: agent-computer interface design
- `MetaGPT`: SOP and artifact-driven team workflow
- `OpenHands SDK`: conversation, workspace, and event boundaries

## Core LeVik Shape

LeVik v1 is:

- a Go host daemon
- a Python orchestrator
- a typed contract between them
- a founder-operated engineering system

LeVik v1 is not:

- a Jarvis-style personal assistant
- a chatty agent swarm
- a Docker-first sandbox platform
- a self-modifying workflow optimizer

## Required Boundaries

### Go owns host execution

The Go daemon should own:

- shell and process execution
- filesystem edits
- git worktree management
- workspace provisioning
- secrets and redaction
- Telegram notifications
- audit/event persistence

### Python owns workflow intelligence

The Python orchestrator should own:

- LangGraph workflow state
- role routing
- model selection
- artifact progression
- approval pauses
- retry and resume behavior

### Shared contract is typed

The Go/Python interface should be based on explicit objects, not implied prompt rules.

Minimum shared object families:

- `TaskSession`
- `HostActionSpec`
- `HostActionRequest`
- `HostObservation`
- `Artifact`
- `ApprovalRequest`
- `ApprovalDecision`
- `TaskStatus`

## Role Model

LeVik should keep stable logical roles:

- `planner`
- `implementer`
- `reviewer`
- `verifier`
- `integrator`

But actual model assignment must remain dynamic and policy-driven.

## Artifact Model

LeVik should pass artifacts, not conversations, across major phases.

Minimum artifact families:

- `TaskSpec`
- `PlanArtifact`
- `ImplementationArtifact`
- `VerificationArtifact`
- `ReviewArtifact`
- `FounderApprovalArtifact`

## Execution Model

The first reliable execution loop should be:

1. intake task
2. clarify or draft spec
3. plan
4. provision worktree
5. implement
6. verify
7. review
8. founder approval if required
9. finalize artifacts

## Non-Goals For V1

- self-organizing model teams
- automatic workflow search
- multi-agent free chat
- benchmark-only optimization
- remote multi-user SaaS architecture

## Immediate Next Build Implication

Before more refactoring, LeVik should define the typed Go/Python execution contract in code and align it with the action, observation, artifact, and approval objects above.

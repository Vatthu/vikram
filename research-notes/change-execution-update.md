# Change Execution Update

## Problem

Vikram had reached `change_ready`, but it still lacked an honest execution seam:

- planning artifacts existed
- verification candidates existed
- no bounded change proposal could yet be applied and validated through the native host contract

That gap is where many systems either:

- blur editing into raw shell behavior
- pretend the orchestrator is already the patch generator
- or mix patching and validation so tightly that failure recovery becomes opaque

## Sources

- Agentless: `https://arxiv.org/abs/2407.01489`
- VRpilot / patch validation feedback: `https://arxiv.org/abs/2405.15690`
- LangGraph persistence and update-state patterns: `https://docs.langchain.com/oss/python/langgraph/persistence`

## What These Sources Solve Well

### Agentless

Agentless reinforces that localization, repair, and validation should stay explicit and inspectable.

For Vikram, that means:

- `change_ready` should remain a preparation state
- actual execution should be a separate step with its own artifacts

### VRpilot

VRpilot reinforces the value of patch validation feedback as a first-class loop component.

For Vikram, the practical v1 lesson is:

- after a bounded edit is applied, verification output must be captured as structured evidence
- the system should not rely on the model “remembering” what happened

### LangGraph

LangGraph’s persisted thread state and `update_state` pattern make it possible to:

- prepare a task in one run
- later inject a change request into the saved thread
- resume execution without rebuilding the whole workflow from scratch

## Vikram Decision

Borrow now:

- bounded exact-text replacement as a host capability
- resumed execution from a saved LangGraph thread
- explicit change artifact and verification-result artifact

Adapt later:

- multi-attempt repair loops
- richer patch selection
- approval gates on risky changes or failed verification

Reject for now:

- unrestricted shell-based patching as the default edit mechanism
- hiding verification inside the same opaque step as editing
- claiming the orchestrator itself is already the autonomous patch generator

## Concrete Impact On Vikram

This step justifies the addition of:

- `POST /v1/files/replace`
- `POST /v1/tasks/{task_id}/changes`

And the execution pattern:

- `change_ready -> apply bounded change request -> run focused verification -> persist evidence`

## Why This Matters

An enterprise-grade engineering team needs a recoverable execution seam between planning and validation.

Vikram now has that seam.

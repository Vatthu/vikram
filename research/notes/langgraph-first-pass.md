# LangGraph First Pass

## Source Snapshot

- local checkout: `research/upstream/langgraph`
- origin: `https://github.com/langchain-ai/langgraph`
- pinned `HEAD`: `45246f6c741f677a405f03e119d7f43466cc2a0b`

## What The Repo Actually Is

`langgraph` is a real Git monorepo, not a small single-package project. At the root it has:

- `docs/`
- `examples/`
- `libs/checkpoint`
- `libs/checkpoint-sqlite`
- `libs/checkpoint-postgres`
- `libs/cli`
- `libs/langgraph`
- `libs/prebuilt`
- `libs/sdk-py`
- `libs/sdk-js`

That structure matches what we care about for LeVik: the runtime graph library, checkpoint backends, and surrounding operational tooling live as separate packages.

## What Problem It Solves Well

- long-running workflow orchestration
- resumable execution after interruption or failure
- explicit state transitions instead of free-form agent chat
- human approval points through interrupts

## Why It Matters For LeVik

This is the clearest upstream reference for the Python orchestrator side of LeVik. It maps directly to:

- task state machines
- checkpointed execution
- pause and resume around founder approvals
- artifact-driven progression instead of conversational agent swarms

## First Extraction Decision

- adopt as a dependency, not as vendored code
- study `libs/langgraph` and `libs/checkpoint-sqlite` first
- ignore hosted ecosystem pieces unless they solve a concrete LeVik v1 problem

## Immediate Cautions

- LangGraph is infrastructure, not the full product architecture.
- It does not replace LeVik’s Go host layer, Telegram gateway, secrets handling, or workspace policy.
- We should borrow workflow discipline from it, not inherit LangChain ecosystem assumptions blindly.

## Next Inspection Targets

1. `libs/langgraph`
2. `libs/checkpoint`
3. `libs/checkpoint-sqlite`
4. `examples/human_in_the_loop`
5. `examples/multi_agent`

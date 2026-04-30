# MetaGPT First Pass

## Source Snapshot

- local checkout: `research/upstream/MetaGPT`
- origin: `https://github.com/geekan/MetaGPT`
- pinned `HEAD`: `11cdf466d042aece04fc6cfd13b28e1a70341b1f`
- related paper: `arXiv:2308.00352`

## What The Repo Actually Is

`MetaGPT` is a broad Python multi-agent framework built around the idea of a software company composed of roles. The local checkout shows:

- `metagpt/software_company.py` as the startup entrypoint for the "software company" flow
- `metagpt/team.py`, `metagpt/roles/`, and `metagpt/actions/` as the collaboration core
- `examples/aflow/` embedded in the repo as a workflow-optimization branch of the project

The local `software_company.py` confirms the core pattern: create a `Team`, hire fixed roles such as product manager, architect, engineer, and data analyst, then run multiple rounds over a shared idea.

## What Problem It Solves Well

- role-oriented task decomposition
- SOP-style progression instead of free-form agent chatter
- artifact production across planning, architecture, and implementation stages
- treating software creation as a staged pipeline instead of a single long conversation

## Why It Matters For LeVik

MetaGPT is one of the clearest references for LeVik’s team semantics:

- roles should exist at the workflow layer, not only as prompt flavor
- intermediate artifacts matter
- staged progression reduces drift better than open-ended multi-agent chat

The paper also supports the same point: it explicitly argues that naive chained multi-agent systems suffer from cascading hallucinations, and positions SOPs plus role specialization as the remedy.

## First Extraction Decision

- borrow the SOP and artifact discipline
- reject the idea of baking a fixed static org chart into LeVik
- adapt the team concept into capability-based role assignment

LeVik should keep stable logical roles such as planner, implementer, reviewer, verifier, and integrator, but select actual models dynamically based on capability and policy rather than hardcoding one vendor to one job.

## Immediate Cautions

- MetaGPT is opinionated and productized around its own abstractions.
- The “software company” metaphor is useful, but too literal if copied directly.
- Its startup flow is still closer to a research framework than to the host-native operator system LeVik needs.

## Next Inspection Targets

1. `metagpt/team.py`
2. `metagpt/roles/`
3. `metagpt/actions/`
4. `metagpt/ext/aflow/`

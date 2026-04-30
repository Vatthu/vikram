# SWE-agent First Pass

## Source Snapshot

- local checkout: `research/upstream/SWE-agent`
- origin: `https://github.com/princeton-nlp/SWE-agent.git`
- pinned `HEAD`: `0f4f3bba990e01ca8460b9963abdcd89e38042f2`
- related paper: `arXiv:2405.15793`

## What The Repo Actually Is

`SWE-agent` is a research-driven software engineering agent project focused on repo work, tool use, and benchmarkable engineering tasks. The local checkout shows:

- `config/` as the main control surface
- `sweagent/agent/`, `sweagent/environment/`, and `sweagent/tools/` as the core runtime
- `tools/` with concrete editing, diff, search, browser, and submission primitives

The README also contains an important current-state note: the team says most new development effort is going into `mini-SWE-agent`, which they recommend going forward because it is simpler while matching performance.

## What Problem It Solves Well

- designing an agent-computer interface for software work
- constraining repo interaction into explicit tools
- repo navigation, editing, testing, and submission as first-class operations
- keeping the LM close to the code instead of trapped in pure chat

The paper backs the same thesis directly: interface design, not just model quality, materially changes software engineering agent performance.

## Why It Matters For LeVik

This is one of the highest-signal references for LeVik’s execution contract:

- a coding agent should operate through a deliberate interface
- repo operations should be explicit
- config-driven tool surfaces are easier to evaluate and revise

## First Extraction Decision

- borrow the ACI mindset aggressively
- adapt the concrete tool contracts into LeVik’s Go host executor and Python workflow steps
- reject benchmark-chasing complexity that does not improve founder-facing reliability

## Immediate Cautions

- The upstream itself points to `mini-SWE-agent` as the simpler forward path, so we should inspect that next before borrowing too deeply from the larger system.
- SWE-agent is more about agent-computer interaction than about multi-agent team management.
- Its default assumptions are benchmark-heavy, while LeVik must optimize for real startup work and operator trust.

## Next Inspection Targets

1. `config/default.yaml`
2. `sweagent/agent/`
3. `sweagent/environment/`
4. `sweagent/tools/`
5. `tools/`

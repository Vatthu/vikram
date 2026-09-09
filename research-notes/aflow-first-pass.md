# AFlow First Pass

## Source Snapshot

- paper: `arXiv:2410.10762`
- local code entrypoint: `research/upstream/MetaGPT/examples/aflow/README.md`

## What The Repo Actually Is

In the local MetaGPT checkout, `AFlow` appears as a focused subsystem rather than a separate large framework. The example README and referenced modules define:

- `Node` as the basic LLM invocation unit
- `Operator` as reusable higher-level building blocks
- `Workflow` as code-represented connected nodes
- `Optimizer` as a Monte Carlo Tree Search process over workflows
- `Evaluator` as the scoring loop that feeds search

The paper frames workflow design as a search problem over code-represented workflows instead of a hand-authored static graph.

## What Problem It Solves Well

- evolving workflows instead of freezing them permanently
- combining execution feedback with workflow optimization
- making workflow structure itself a tunable object

## Why It Matters For Vikram

Vikram v1 should not self-modify its orchestration logic automatically. That would be premature and difficult to trust.

But AFlow is important for Vikram v2 or v3 because it suggests a disciplined future direction:

- keep workflow definitions explicit and code-represented
- keep evaluation artifacts structured
- make it possible to compare alternate workflow designs systematically later

## First Extraction Decision

- reject automatic workflow search for Vikram v1
- borrow the idea that workflows should be explicit, evaluable, and versionable
- adapt evaluation hooks so future workflow optimization is possible without rewriting the system

## Immediate Cautions

- AFlow is an optimization layer, not a substitute for a stable baseline architecture.
- Automatic workflow search without strong evals is noise.
- Vikram needs high-quality human-designed workflow baselines before it should attempt workflow self-optimization.

## Next Inspection Targets

1. `metagpt/ext/aflow/`
2. `examples/aflow/optimize.py`
3. evaluation and benchmark hooks inside the AFlow codepath

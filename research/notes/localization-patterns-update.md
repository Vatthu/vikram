# Localization Patterns Update

## Problem

LeVik had repository inspection, but it still lacked a strong answer to:

- where should the agent look next
- how should it narrow candidate files before editing
- how can it avoid turning repo understanding into raw-shell wandering

## Sources

- Agentless: `https://arxiv.org/abs/2407.01489`
- AgentFL: `https://arxiv.org/abs/2403.16362`
- LingmaAgent / RepoUnderstander: `https://arxiv.org/abs/2406.01422`
- Improving Code Localization with Repository Memory: `https://arxiv.org/abs/2510.01003`

## What These Sources Solve Well

### Agentless

Agentless argues that a simple explicit workflow can beat more complex agent loops:

- localization
- repair
- patch validation

The important lesson for LeVik is not “remove agents”.
The lesson is “do not skip localization”.

### AgentFL

AgentFL decomposes project-level localization into:

- comprehension
- navigation
- confirmation

This reinforces that codebase navigation should be a first-class stage, not an incidental side effect of tool calls.

### LingmaAgent

LingmaAgent shows that repository-level understanding matters.

The strongest relevant lesson for LeVik is:

- repository exploration must be broader than local-file guessing
- planning quality improves when the agent has structured repo context before patching

### Repository Memory

The 2025 repository-memory work points at the next weakness after localization:

- each task starts from scratch
- commit history and repeated repository knowledge are underused

This is not a v1 requirement, but it is a strong v2 direction.

## LeVik Decision

Borrow now:

- explicit localization before implementation
- bounded repository navigation
- target-file ranking
- bounded file previews instead of whole-repo dumping

Adapt later:

- repository memory from commit history
- richer repo summaries
- iterative search and backtracking

Reject for now:

- full MCTS
- knowledge-graph heavy repo modeling
- chat-heavy navigation loops

## Concrete Impact On LeVik

This note justifies the addition of:

- `POST /v1/repos/discover-targets`
- `POST /v1/files/read`

And the workflow transition:

- `repo_inspected -> targets_discovered -> implementation_ready`

## Why This Matters

LeVik is trying to become an engineering team, not a demo agent.

That means it should:

- localize before acting
- read narrowly before editing
- plan from evidence, not only from prompts

These papers all push in that direction, even when their runtime designs differ.

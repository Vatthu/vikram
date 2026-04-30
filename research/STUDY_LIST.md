# Study List

## Cloned Frameworks

- `langgraph`: workflow durability, checkpoints, interrupts, graph patterns
- `openclaw`: routing, agent isolation, Telegram and control-plane ideas
- `MetaGPT`: SOPs, role decomposition, software-company artifact flow
- `SWE-agent`: agent-computer interface, repo navigation, bounded tool interface design
- `software-agent-sdk`: current OpenHands agent core, conversation/workspace split, event model
- `adk-python`: sessions, A2A patterns, tool confirmation, agent graphs
- `OpenHands`: coding-runtime and workspace patterns
- `agent-framework`: orchestration patterns, telemetry ideas, enterprise tradeoffs

## Small Focused Repos To Add Next

- `mini-SWE-agent` as the likely higher-signal successor to `SWE-agent`
- official MCP protocol and SDK repositories
- focused browser-automation and verifier repos
- narrow repos that solve artifact evaluation, code review, or task recovery especially well

## Paper Tracks

- multi-agent software engineering
- durable agent workflows
- verifier and reviewer design
- human-in-the-loop orchestration
- tool safety and constrained autonomy

## Research Workflow

1. Verify the local clone in `research/upstream/MANIFEST.md`.
2. Read the upstream root `README.md` and inspect the actual top-level structure.
3. Identify the specific module or docs area that matters for LeVik.
4. Write a note under `research/notes/` before proposing a design change.
5. Decide whether LeVik should adopt the pattern, adapt it, or reject it.

## Output Standard For Research

Every study note should answer:

1. What problem does this source solve well?
2. Is that problem real for LeVik v1 or only later?
3. Do we borrow the pattern, clone the idea, or ignore it?
4. What is the concrete impact on LeVik’s architecture?

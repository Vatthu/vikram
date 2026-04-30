# Framework Gap Analysis

This is a decision record for how LeVik compares to the major frameworks and papers we are using as inputs.

## Measurement Rule

The percentages below are not benchmark claims.

They are heuristic estimates of useful capability coverage for LeVik's target system:

- host-native engineering execution
- durable orchestration
- founder-operated approvals and notifications
- artifact-driven workflow
- software-engineering-specific tooling

`100%` means a system would already behave like the founder-operated engineering team we want to build.

## Current Score

Current LeVik after the native host, worktree, and first-plan-artifact step:

- `~30%` of end-state system power
- `~65%` of the correct architectural direction

Reason:

- the core boundary is now correct
- the first host-native workflow is real
- implementation, verification, approval-resume, and richer artifact flow are still missing

## Side-by-Side View

### LangGraph

- `~80%` of the orchestration problem
- `~55%` of total LeVik target fit by itself

What it gives:

- durable execution
- checkpoints
- interrupts and resume
- workflow graphs

What it does not give by itself:

- host daemon
- repo-native engineering tools
- founder operations layer
- worktree and artifact governance

### OpenHands SDK

- `~75%` of the software-agent runtime pattern problem
- `~45%` of total LeVik target fit by itself

What it gives:

- agent and conversation model
- workspace abstraction
- strong software-engineering orientation
- remote agent server pattern

What it does not give by itself:

- founder-first operating model
- host-native Mac control plane
- strict Go-owned host execution boundary
- LeVik-specific artifact and approval policy

### Microsoft Agent Framework

- `~75%` of enterprise orchestration surface area
- `~50%` of total LeVik target fit by itself

What it gives:

- orchestration patterns
- sessions
- middleware
- checkpointing and graph workflows
- enterprise-friendly breadth

What it risks:

- overbuilding the system too early
- drifting into chat-heavy orchestration if group chat or handoff becomes the default pattern

### MetaGPT

- `~70%` of the workflow-discipline lesson
- `~40%` of total LeVik target fit by itself

What it gives:

- SOP discipline
- role separation
- artifact mindset

What it does not give:

- the right host boundary
- low-token execution primitives
- founder-operated control plane

### OpenClaw

- `~70%` of the operator gateway and channel-routing lesson
- `~40%` of total LeVik target fit by itself

What it gives:

- channel and operator ergonomics
- multi-agent routing ideas
- practical control-plane patterns

What it does not give:

- LeVik's typed workflow brain
- artifact-first engineering loop
- our exact host-native execution model

## Why LeVik Is Not Lower Than This

LeVik is still early, but the hard architectural mistakes are being avoided:

- we did not let Python own host execution
- we did not let agents free-chat by default
- we did not build Docker-in-Docker around the control plane
- we did not hardcode model vendors into permanent roles

That means the current system is weaker than the big frameworks in raw feature count, but stronger in target alignment.

## What The Papers Add That Frameworks Still Miss

### SWE-agent

LeVik should borrow the ACI lesson more aggressively than most frameworks do:

- LM-friendly interfaces matter as much as model intelligence
- post-action state probes matter
- observation shaping matters

Most frameworks expose orchestration primitives, but they do not hand you a software-engineering-specific action interface out of the box.

### MetaGPT

The paper contributes an operating doctrine, not only a library:

- SOP encoding
- assembly-line artifact flow
- role discipline to reduce cascading errors

Most frameworks provide runtime primitives, not a strong software-delivery doctrine.

### ChatDev

The useful part is not the chat swarm itself.

The useful part is:

- communication protocol matters
- dehallucination rules matter
- different communication forms are better for different phases

Most frameworks do not enforce communication discipline. They let users build expensive chatter.

### AFlow

This is the biggest future-facing gap:

- workflow design itself can be optimized
- smaller models can outperform larger ones inside a better workflow

Most current frameworks help you define workflows.
They do not help you search for better workflows in a principled way.

## LeVik Adoption Rule

Use now:

- LangGraph durability
- Go-owned host actions
- task worktrees
- artifact-first phase boundaries
- policy-based model selection

Adopt later:

- richer state probes
- verification artifacts
- approval-resume loop
- model-routing evaluation

Delay to v3 or later:

- workflow self-optimization
- self-proposed team reconfiguration
- high-autonomy multi-agent parallelism

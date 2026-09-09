# SWE-agent Extraction

## Source Slice

- `config/default.yaml`
- `docs/background/aci.md`
- `docs/config/tools.md`
- `sweagent/agent/agents.py`
- `sweagent/tools/tools.py`

## High-Signal Patterns

### 1. The interface is data-driven

SWE-agent does not treat tools as a hidden prompt convention. It models them as configuration:

- prompt templates
- tool bundles
- environment variables
- parser choice
- state commands
- blocklists and timeouts

This is the right pattern for Vikram. The execution interface should be declared data-first and inspected by code, not embedded inside one giant agent prompt.

### 2. Post-action state matters

The tool system supports `state_command` hooks that run after actions and return structured JSON, such as current working directory or open file. That is a strong idea.

For Vikram, every host action should have optional state probes, such as:

- current repo root
- worktree path
- git dirty state
- last command exit code
- active task artifact paths

### 3. Observation shaping is part of the interface

SWE-agent explicitly handles:

- empty command output
- truncated observations
- bash syntax errors
- command blocklists

This is not a cosmetic detail. It reduces failure loops and lowers prompt ambiguity.

### 4. Narrow, purpose-built tools beat raw shell sprawl

The ACI docs emphasize that custom viewers, search tools, and guarded editors outperform naive `cat` plus free-form shell usage.

## What Vikram Should Borrow

- a declarative host action registry
- structured post-action state probes
- explicit timeout and blocklist policy
- observation shaping rules for empty, truncated, and invalid outputs
- a small set of purpose-built repo tools before opening broad shell access

## What Vikram Should Adapt

- Replace SWE-agent tool bundles with a Vikram host action registry owned by the Go daemon.
- Map `state_command` into Go-side state probes and Python-side state updates.
- Keep the shell available, but make it a lower-trust tool than first-class repo actions.

## What Vikram Should Reject

- benchmark-specific complexity that exists mainly for SWE-bench
- anthropic-specific or upstream-specific editing assumptions
- any tool surface that is larger than necessary for the first reliable Vikram loop

## Concrete Vikram Impact

Vikram should gain a `HostActionSpec` model with fields like:

- `name`
- `kind`
- `arguments_schema`
- `approval_policy`
- `timeout_seconds`
- `state_probe`
- `observation_policy`
- `side_effect_level`

This should live at the Go/Python boundary, not only in prompts.

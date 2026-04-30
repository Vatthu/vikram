# Host-Native Architecture

## Position

LeVik runs directly on a dedicated macOS machine.

The control plane is not container-first. This avoids Docker-in-Docker and keeps LeVik close to the actual development environment it is managing.

## Process Model

LeVik v1 uses two long-running local services:

- `levikd` in Go
- `levik-orchestrator` in Python

Optional worker processes may be launched by the host layer for:

- shell execution
- git worktree operations
- browser verification
- model-specific CLI workers

## Responsibility Split

Go owns:

- Telegram and other future gateways
- permissions and safety checks
- host execution APIs
- workspace provisioning
- audit logging
- secret storage
- notifications

Python owns:

- workflow graphs
- state transitions
- model routing
- approval logic
- task decomposition
- artifact evaluation

## Storage Layout

Suggested runtime layout under `~/.levik/`:

- `run/` for sockets and pid files
- `db/` for SQLite files
- `logs/` for structured logs
- `tasks/` for task metadata
- `artifacts/` for plans, diffs, tests, and reports
- `workspaces/` for provisioned repo worktrees
- `secrets/` for local encrypted state when Keychain is not yet used

## Dedicated macOS User

This is not mandatory for local development.

For the production Mac mini, a dedicated macOS user is strongly recommended because it gives:

- cleaner separation from your personal account
- easier audit boundaries
- safer secret handling
- fewer accidental repo and SSH crossovers
- a clearer “this machine works for LeVik” operating model

Tradeoff:

- file sharing becomes explicit
- you need a handoff path when you want to inspect or move artifacts between your account and LeVik’s account

Middle ground:

- develop under your current user now
- deploy under a dedicated `levik` macOS user on the production Mac mini

## Docker Position

LeVik itself is not run inside Docker.

If a target project needs Docker, LeVik may invoke Docker on the host as a normal project tool. That is acceptable because the project is using Docker, not LeVik nesting its own control plane inside Docker.

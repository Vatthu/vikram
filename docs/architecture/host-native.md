# Host-Native Architecture

## Position

Vikram runs directly on a dedicated macOS machine.

The control plane is not container-first. This avoids Docker-in-Docker and keeps Vikram close to the actual development environment it is managing.

## Process Model

Vikram v1 uses two long-running local services:

- `vikramd` in Go
- `vikram-orchestrator` in Python

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

Suggested runtime layout under `~/.vikram/`:

- `run/` for sockets and pid files
- `db/` for SQLite files
- `logs/` for structured logs
- `tasks/` for task metadata
- `artifacts/` for plans, diffs, tests, and reports
- `workspaces/` for provisioned repo worktrees
- `secrets/` for local encrypted state when Keychain is not yet used

The launchd installer now creates the production layout with private
permissions:

- `run/vikramd.sock` for the Go host API
- `run/vikram-orchestrator.sock` for the Python orchestrator API
- `secrets/console-api-key` with `0600` permissions
- `bin/vikram-gateway-wrapper.sh` with `0700` permissions
- `logs/gateway.log` and `logs/gateway.err` for daemon output

Install without starting the daemon:

```bash
contrib/install-daemon.sh --no-load
```

Install and start under launchd:

```bash
contrib/install-daemon.sh
```

The launchd plist runs the wrapper directly, not through `sh -c`. The wrapper
sets stable socket paths and reads the console API key from the private secrets
file before starting `vikram gateway`.

## Dedicated macOS User

This is not mandatory for local development.

For the production Mac mini, a dedicated macOS user is strongly recommended because it gives:

- cleaner separation from your personal account
- easier audit boundaries
- safer secret handling
- fewer accidental repo and SSH crossovers
- a clearer “this machine works for Vikram” operating model

Tradeoff:

- file sharing becomes explicit
- you need a handoff path when you want to inspect or move artifacts between your account and Vikram’s account

Middle ground:

- develop under your current user now
- deploy under a dedicated `vikram` macOS user on the production Mac mini

## Docker Position

Vikram itself is not run inside Docker.

If a target project needs Docker, Vikram may invoke Docker on the host as a normal project tool. That is acceptable because the project is using Docker, not Vikram nesting its own control plane inside Docker.

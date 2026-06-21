# Repository Layout

## Active Areas

```text
vikram/
├── cmd/
│   └── vikram/                  # Go CLI / daemon entrypoints
├── pkg/                        # Go host-layer packages
├── services/
│   └── orchestrator/           # Python orchestration service
├── docs/
│   └── architecture/           # architecture decisions and contracts
├── research/
│   ├── upstream/               # cloned upstream repos for study
│   └── papers/                 # paper notes and references
└── archive/                    # legacy material and prototypes
```

## Go Side

The active direction for the Go side is:

- gateway and Telegram integration
- host execution APIs
- workspace and git worktree management
- permissions and safety controls
- secret storage
- audit and event transport

The Go codebase still contains legacy assistant-era packages. They should be evaluated package by package before further cleanup.

## Python Side

The Python side starts fresh under `services/orchestrator/`.

That service owns:

- workflow graph construction
- task lifecycle state
- model capability routing
- approvals and resume behavior
- artifact generation and evaluation

## Research Side

The `research/` tree exists to keep study material local and persistent.

It should not become a place where production code is copied wholesale from upstream frameworks.

## Archive Rule

If code or docs are moved to `archive/`, they are out of the active build path.

They may still contain useful ideas, but they are not part of the working foundation unless deliberately reintroduced.

# Vikram Roadmap

## Completed

### Phase 1: Architecture Foundation
- Go host daemon with Unix socket API (21 endpoints)
- Python LangGraph orchestrator (31-node workflow)
- Shared typed contract between Go and Python
- Git worktree isolation per task
- SQLite checkpointing for state persistence
- 20+ LLM provider support

### Phase 2: Multi-Agent Team
- Config-driven role assignment (lead, engineer, reviewer, runner, qa)
- Per-role provider and model selection
- Runtime agent registration via opt-in local management console
- Subagent spawning with independent tool loops
- Team role descriptions injected into system prompts

### Phase 3: Verification Pipeline
- Adversarial spec validation (Devil's Advocate attacks plan → lead revises)
- Pre/post-edit lint guard with new-error diffing
- Automated test execution with exit code capture
- Independent LLM review (different model from implementer)
- Risk classification policy feeding approval gates

### Phase 4: Safety & Reliability
- Transactional worktree rollback for managed task worktrees via `git reset --hard` and `git clean -fd`
- OS-level command allowlisting (48 safe commands, 13 deny patterns)
- Path containment and .git traversal blocking
- Budget enforcement with Telegram notifications
- Council fallback on LLM errors with cooldown caching
- Crash recovery checkpoints before every LLM call

### Phase 5: Production Infrastructure
- Production-grade message bus (subscriber isolation, bounded memory)
- Observation shaping (SWE-agent templates)
- History compression for context window management
- Opt-in local management console with CRUD (agents, providers)
- Dashboard with system status
- Team summary notifications
- Launchd daemon config for macOS

## In Progress

### Phase 6: External Tool Integration
- MCP client support (protocol, process management, concurrent calls)
- Per-tool timeout and output capping
- Allowlist filtering for external tools

### Phase 7: Web Interface
- Full management application (not terminal-only)
- Task lifecycle monitoring
- Approval/reject workflow from browser
- Provider and agent configuration UI

## Planned

### Phase 8: End-to-End Testing
- Full pipeline test with real models
- Load testing under concurrent tasks
- Multi-day autonomous runs

### Phase 9: Visual QA
- Playwright-based browser testing via constrained MCP runner
- Screenshot diffing and visual regression
- QA role with vision model integration

### Phase 10: Production Hardening
- Log rotation and monitoring
- Metrics and alerting
- Automated restart on crash
- Multi-machine worker distribution

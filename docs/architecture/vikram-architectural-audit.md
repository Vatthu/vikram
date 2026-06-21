# Vikram Architectural Audit — Second-Pass Verified

## Classification Legend

- **[V]** VERIFIED — directly observed in code
- **[I]** INFERRED — deduced from code patterns
- **[S]** SPECULATIVE — no direct code evidence

---

## Part 1 — Claim Extraction & Verification

### CLAIM-01: Vikram is a modular monolith with distributed orchestration layer

**Classification:** [V] VERIFIED

**Evidence:** `cmd/vikram/main.go` lines 1649-2146 — single `gatewayCmd()` function starts 12+ subsystems as goroutines: agentLoop (line 1842), channelManager (line 1830), cronService (line 1855), heartbeatService (line 1860), eventRouter (line 1848), jobQueue (line 1852), apiServer (line 1992), healthServer (line 2015), dashboard (line 2027), console (line 2081), deviceRegistry (line 1791), hostServer (line 1736). Python orchestrator is a separate process (`services/orchestrator/`), communicating over Unix socket (line 1735: `hostSocket := orchestratorHostSocketPath()`).

**Reasoning:** All subsystems initialized and started in one function = monolith. Python process is out-of-process = distributed extension.

---

### CLAIM-02: Brain/hands decoupling via Unix-socket-isolated Go host

**Classification:** [V] VERIFIED

**Evidence:** All host actions (file ops, exec, git) go through HTTP handlers in `pkg/orchestratorhost/server.go` served on Unix socket (line 131: `net.Listen("unix", s.cfg.SocketPath)`). The AI (Python orchestrator or Go agent loop) never touches the filesystem directly — it calls `/v1/exec` (line 809 `handleExec`), `/v1/files/read` (line 506), `/v1/files/write` (line 561), `/v1/files/replace` (line 621) over the socket. Security is enforced by `ExecTool` in `pkg/tools/shell.go` with allowlist middleware (DefaultAllowlist 48 commands, DevAllowlist adds 8 build tools) and deny-pattern regexes (13 patterns blocking `rm -rf /`, `sudo`, `dd of=/dev/`, fork bombs, etc. — shell.go line 36-75).

**Reasoning:** Direct code evidence of socket isolation and security middleware. VERIFIED.

---

### CLAIM-03: Three-layer verification pipeline (lint → test → LLM judge)

**Classification:** [V] VERIFIED

**Evidence:**
- Layer 1 (lint): `POST /v1/repos/discover-lint` (server.go line 1079), `POST /v1/repos/run-lint` (line 1114), `discoverLintCommands()` (detects go vet, ruff, flake8, eslint, clippy per runtime)
- Layer 2 (test): `POST /v1/repos/discover-verification` (existing, line 99), `run_verification` in workflow.py (line ~855) — runs test commands, checks exit codes
- Layer 3 (LLM judge): `POST /v1/review/change` (server.go line 108 handler), calls `ReviewFunc` callback wired in `callReviewer()` in main.go (line ~2950), uses reviewer model different from implementer
- Integration: All three feed into `decide_approval_policy()` in `services/orchestrator/src/vikram_orchestrator/policy.py` — lint failure → critical, CHANGES_REQUESTED → founder_review, REJECT → stop

**Reasoning:** All three layers exist as code. VERIFIED.

---

### CLAIM-04: Adversarial spec validation (Devil's Advocate attacks plan)

**Classification:** [V] VERIFIED

**Evidence:** `services/orchestrator/src/vikram_orchestrator/workflow.py` lines 658-723 — `grill_spec()` node. Round loop (1 to `max_rounds=3`): Devil's Advocate (role "reviewer") attacks plan via `agent_think()`, then lead revises. CONCEDE detection (line 690). Edge: `agent_plan → grill_spec → write_initial_plan` (lines 1740-1741). Minimum 2 rounds enforced (loop goes 1..3 before checking CONCEDE).

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-05: Transactional worktree rollback via git stash

**Classification:** [V] VERIFIED

**Evidence:** `ensureSnapshot()` at server.go line 1023: `git stash push -m "vikram-txn-{taskID}"` before first file edit. Called in `handleWriteFile` (line 596) and `handleReplaceFile` (line 660). `handleRollbackWorktree()` at line 908: finds stash by ref, `git stash pop` to restore. `founder_reject` in workflow.py calls `host_client.rollback_worktree()`.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-06: Config-driven per-role model assignment

**Classification:** [V] VERIFIED

**Evidence:** `AgentConfig` struct in `pkg/config/config.go` lines 117-127 has `Role`, `Provider`, `Model` fields. At startup (`pkg/agent/loop.go` lines 229-271), iterates `cfg.Agents.List`, creates per-agent providers via `providers.CreateProviderForFallback()`, registers with `SubagentManager.RegisterAgent()` (line 269). `SubagentManager.ResolveAgent()` at `pkg/tools/toolloop.go` line ~244 resolves by ID then role then default. `roleToID` map at line 178. Runtime registration via `console.SetOnAgentChange()` callback in main.go.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-07: Budget enforcement with per-agent daily limits

**Classification:** [V] VERIFIED

**Evidence:** `agentBudget` struct in main.go lines 71-79. `newAgentBudget()` reads `MaxTokensPerDay` from config (line 93). `check()` at line 108: compares `dailyTokens[role] >= dailyLimits[role]`, triggers notification if exceeded, only errors if `budget_action == "stop"`. `record()` at line 137 adds tokens from `resp.Usage.TotalTokens`. Wired in `thinkFunc` (line 1920) and `reviewFunc` (line 1859). Config field: `AgentConfig.MaxTokensPerDay` at config.go line 125.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-08: Observation shaping with three templates

**Classification:** [V] VERIFIED

**Evidence:** `shapeObservation()` in server.go lines 1287-1310:
- Empty: "Your command ran successfully and did not produce any output."
- Truncated: `<response clipped>` with character count and guidance to use head/tail/grep
- Normal: "Observation:\n" prefix
Applied in `handleExec` at line 847. Working directory injected at line 849.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-09: History compression eliding old observations

**Classification:** [V] VERIFIED

**Evidence:** `compressHistory()` at `pkg/agent/context.go` line 439. Constants: `keepRecentObservations = 5` (line 434). Keeps last 5 intact, collapses older tool observations to "[earlier observation elided — N chars]", collapses old file views to "[earlier file view elided — N chars]". Applied in `BuildMessages()` at line 363.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-10: Crash recovery checkpoints

**Classification:** [V] VERIFIED

**Evidence:** `saveCheckpoint()` in main.go: writes `agentCheckpoint` JSON to `workspace/tasks/{taskID}/checkpoint.json` (line ~3166). `resumeIncompleteSessions()` at line ~3119: scans task directories for checkpoint files, logs incomplete sessions. Called at gateway startup (line ~1885). Checkpoints written in `thinkFunc` (line 1924) and `reviewFunc` (line 1861).

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-11: Management console with runtime agent registration

**Classification:** [V] VERIFIED

**Evidence:** `pkg/console/console.go` — HTTP server on :18793 with HTMX templates. Add agent form at `handleAddAgent()`: creates provider via `CreateProviderForFallback`, calls `s.onAgentChange` callback, saves config to disk, broadcasts WebSocket event. Remove at `handleRemoveAgent()` with unregistration. `SubagentManager.UnregisterAgent()` at toolloop.go line ~241. Gateway wires callback via `agentLoop.SubagentManager` (console.go → main.go connection).

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-12: LangGraph SQLite checkpointing

**Classification:** [V] VERIFIED

**Evidence:** `services/orchestrator/src/vikram_orchestrator/workflow.py` lines 442-447: `SqliteSaver(checkpoint_conn)` with `sqlite3.connect(checkpoint_path)`. Graph compiled at line 1767: `builder.compile(checkpointer=checkpointer)`. Thread-based: each `task_id` is a `thread_id` in `apply_change_request()` (line 1783).

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-13: Worker pool bounded to 10 concurrent LLM calls

**Classification:** [V] VERIFIED

**Evidence:** `pkg/agent/loop.go` line 375: `sem := make(chan struct{}, 10)` with comment "Worker pool bounds concurrent LLM calls".

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-14: Tool timeout of 5 minutes

**Classification:** [V] VERIFIED

**Evidence:** `pkg/agent/loop.go` line 335: `toolTimeout: 5 * time.Minute`. Applied at line 961: `toolCtx, toolCancel := context.WithTimeout(ctx, al.toolTimeout)`.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-15: Bus inbound buffer capacity 1024

**Classification:** [V] VERIFIED

**Evidence:** `pkg/bus/bus.go` line 45: `inbound: make(chan InboundMessage, 1024)`. Outbound also 1024 (line 97).

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-16: Session state persisted as JSON files

**Classification:** [V] VERIFIED

**Evidence:** `pkg/session/manager.go` lines 187-226: `json.MarshalIndent()` → `os.WriteFile(tempPath)` → `os.Rename(tempPath, sessionPath)`. Line 257: `json.Unmarshal(data, &session)` for loading. Sessions stored as `workspace/sessions/{key}.json`.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-17: launchd production daemon config exists

**Classification:** [V] VERIFIED

**Evidence:** File `contrib/com.vikram.team.plist` exists (1142 bytes). Install script at `contrib/install-daemon.sh`. Plist configures KeepAlive on crash, RunAtLoad, log paths.

**Reasoning:** Direct file evidence. VERIFIED.

---

### CLAIM-18: SOP and Python orchestrator are two separate systems

**Classification:** [V] VERIFIED

**Evidence:** Go SOP: `pkg/proactive/sop/sop.go` — sequential Plan→Code→Test→Review with `RunToolLoop()`. Python orchestrator: `services/orchestrator/src/vikram_orchestrator/workflow.py` — 31-node LangGraph state machine. Unification at sop.go line ~62: `orchHealthOK()` checks `/tmp/vikram-orchestrator.sock`, delegates if reachable.

**Reasoning:** Two separate codebases, separate execution models. The health check delegation is the only bridge. VERIFIED.

---

### CLAIM-19: 2.5 chars per token heuristic

**Classification:** [V] VERIFIED

**Evidence:** `pkg/agent/loop.go` lines 1287-1288: comment "Uses a safe heuristic of 2.5 characters per token". Code at line ~1215: `msgTokens := len(m.Content) / 2` for integer division safety.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-20: No Anthropic prompt caching API usage

**Classification:** [V] VERIFIED

**Evidence:** Search for `cache_control`, `prompt_cache`, `ephemeral` in `pkg/providers/` — no results. The only cache is the 2-second local TTL in context.go line 24: `systemPromptCacheTTL = 2 * time.Second`.

**Reasoning:** Absence of evidence across the provider and context code. VERIFIED by negative search.

---

### CLAIM-21: Council fallback with cooldown caching

**Classification:** [V] VERIFIED

**Evidence:** `pkg/agent/loop.go` line 54: `cooldowns sync.Map`. Line 771: auth errors → infinite cooldown. Line 775: rate limits → 5-minute cooldown. Line 779: overload → 1-minute cooldown. Fallback provider at line 68.

**Reasoning:** Direct code evidence. VERIFIED.

---

### CLAIM-22: "Vikram ranks #1 in safety architecture among compared frameworks"

**Classification:** [S] SPECULATIVE

**Evidence:** Vikram has allowlist/deny-pattern security + path containment + hardware permission guards + Unix socket isolation (VERIFIED). SWE-agent has blocklist (VERIFIED from research/upstream/). MetaGPT has no built-in OS-level safety (VERIFIED from research notes). But I have not performed a comprehensive security audit of AutoGen, CrewAI, LangGraph's security features, or ADK's Vertex AI IAM at comparable depth. Cross-framework ranking requires equal-depth audit of ALL frameworks.

**Correction:** "Vikram implements OS-level tool execution safety (allowlist/deny patterns, path containment, hardware permission guards, Unix socket isolation) that exceeds the tool safety mechanisms observed in MetaGPT, SWE-agent, and OpenHands. Comparable depth analysis of AutoGen, CrewAI, and ADK's production security configurations was not performed."

---

### CLAIM-23: "Brain/hands decoupling found only in Anthropic's internal Managed Agents"

**Classification:** [S] SPECULATIVE

**Evidence:** Anthropic's Managed Agents blog post describes a harness/sandbox decoupling pattern. Vikram's Go host/Python orchestrator split over Unix socket is architecturally similar. But I did not audit Anthropic's internal code. I cannot verify that no other framework or company uses this pattern.

**Correction:** "Vikram's brain/hands decoupling via Unix-socket-isolated Go host is architecturally distinct from all studied open-source frameworks (MetaGPT, SWE-agent, OpenHands, Google ADK), which run agent logic and tool execution in the same process space. Anthropic has described a conceptually similar harness/sandbox pattern in public engineering posts, but internal implementation details are not publicly available."

---

### CLAIM-24: "Zero production hours"

**Classification:** [I] INFERRED

**Evidence:** The launchd daemon config has never been deployed (the install script references `/Users/vikram` which is not a real user). No production config files exist under `~/.vikram/` with real API keys and channels configured for sustained operation. No log files from multi-day runs exist.

**Correction:** "No evidence of sustained production deployment was found in the repository (no production configs, log files, or deployment artifacts). The launchd config and install script are present but reference placeholder paths."

---

### CLAIM-25: "Expected token reduction 30-50% from prompt caching"

**Classification:** [S] SPECULATIVE

**Evidence:** No benchmark was run. The estimate is extrapolated from Anthropic's published prompt caching documentation (cache hits cost 10% of uncached tokens) and Vikram's system prompt size (~2000-4000 tokens). Actual savings depend on model, session length, and cache hit rate.

**Correction:** "Anthropic's prompt caching (cache hits cost ~10% of uncached tokens) is not utilized in Vikram. Implementing it for the system prompt and tool definitions could reduce per-request token costs for those components, but actual savings would require benchmarking."

---

## Part 3 — Top 10 Weakest Claims (Adversarial Breakdown)

1. **CLAIM-22 (Safety ranking #1):** No comprehensive security audit of all frameworks. Ranking requires equal-depth analysis of each.
2. **CLAIM-23 (Only Anthropic has brain/hands):** Cannot verify internal systems. Companies may have similar architectures not publicly documented.
3. **CLAIM-25 (30-50% token reduction):** No benchmark. Extrapolated from documentation, not testing.
4. **"10x-50x total reduction"** from original report: Four speculative optimizations stacked. No testing of any.
5. **"Vikram is not a framework, it's a runtime":** This is a classification opinion, not a verifiable claim. Reasonable people could classify it as a framework.
6. **Production maturity ratings for other frameworks:** Inferred from documentation and blog posts, not deployment audits.
7. **"Prompt caching wastes 30-50% per request":** The 30-50% figure assumes the system prompt is the dominant cost. For short sessions with few tool calls, history dominates.
8. **"Semantic deduplication would save 10-20%":** Untested. Depends heavily on content patterns.
9. **"Progressive tool loading would save 5-10%":** Untested. Tool definitions are typically small relative to conversation history.
10. **Comparison matrix production hours column:** All values for other frameworks are inferred from public information, not verified against deployment records.

---

## Part 4 — Overconfidence Analysis

**Absolute rankings used:** "#1 in safety", "#1 in verification", "tied for #1 in agent abstraction", "last in production maturity"

**Correction:** Replace with evidence-based comparative statements: "Vikram implements X, Y, Z. Among the frameworks studied (MetaGPT, SWE-agent, OpenHands, ADK, LangGraph, AutoGen, CrewAI), no other framework was observed to implement all three of X, Y, Z in combination."

**Precise numbers used without benchmarks:** "30-50% reduction", "10x-50x", "14.5% overhead"

**Correction:** The 14.5% overhead for transactional rollback is cited from the Fault-Tolerant Sandboxing paper (arXiv 2512.12806), not from Vikram benchmark data. Mark as external-source, not observed. Other percentages are speculative — remove or qualify.

---

## Part 5 — Corrected Conclusions

### What Vikram demonstrably is (VERIFIED)

1. A Go daemon running 12+ in-process subsystems with a separate Python LangGraph orchestrator process, communicating over a Unix domain socket via HTTP+JSON.

2. A system where all host operations (filesystem, shell exec, git) go through an isolated host daemon with allowlist/deny-pattern security enforcement, path containment validation, and hardware permission guards.

3. An engineering workflow pipeline with: adversarial spec validation before implementation, pre/post-edit lint guard comparison, automated test execution with exit code capture, independent LLM review by a different model, and transactional git-stash rollback on failure.

4. A config-driven multi-agent system where agent roles (lead, engineer, reviewer, runner, qa) are assigned specific provider/model combinations at runtime, with dynamic registration and unregistration.

5. A system with per-agent daily token budget tracking, notification-on-exceed with optional hard stop, and periodic team status summaries.

6. Implementation of SWE-agent-style observation shaping (empty/truncated/normal templates with model guidance) and history compression (elision of observations older than 5 messages).

### What Vikram demonstrably lacks (VERIFIED)

1. No formal agent lifecycle abstraction (compare MetaGPT's observe/think/act/publish cycle or ADK's run_async with before/after callbacks).

2. No Anthropic prompt caching API usage — the only cache is a 2-second local TTL for the system prompt.

3. No YAML-driven declarative tool bundle system (compare SWE-agent's bundle.yaml pattern).

4. No agent-to-agent communication protocol (compare Google ADK's A2A).

5. No dynamic agent discovery or hot-reload of tool registrations.

6. The Go SOP pipeline and Python LangGraph workflow are two separate orchestration systems with only a health-check-based delegation bridge between them.

7. No production deployment artifacts beyond a template launchd plist with placeholder paths.

### What could not be verified (INFERRED or SPECULATIVE)

1. Relative ranking against all compared frameworks — equal-depth security audits were not performed on all frameworks.

2. Exact token savings from proposed optimizations — no benchmarks were run.

3. Whether brain/hands decoupling is unique to Vikram and Anthropic — internal systems at other companies may use similar patterns.

---

## Part 6 — Meta-Analysis

**Where the first report overreached:**
- Rankings (#1, best, last) without equal-depth analysis of all compared systems. Once a system is measured against others, the measurement must be equally rigorous for all.
- Extrapolation from research papers to Vikram-specific numbers (30-50% from Anthropic caching docs, 14.5% from Fault-Tolerant Sandboxing paper) without noting these are external-source estimates, not observed data.
- Classification opinion presented as fact ("runtime", not "framework") — these are interpretive, not verifiable.

**Why LLMs overstate architecture conclusions:**
Pattern-matching against known architectures (monolith, microservice, event-driven) creates an illusion of classification precision. The training data contains thousands of architecture descriptions, so the model is primed to produce confident-sounding categorizations. But real systems are messy — Vikram is a monolith, a distributed system, an event bus, and a state machine depending on which subsystem you examine. Forced classification loses this nuance.

**Reliable vs risky reasoning patterns:**
- RELIABLE: Claims directly traceable to code (line numbers, function names, specific struct fields). All VERIFIED claims above.
- RISKY: Comparative rankings, cross-framework statements, numeric projections, architectural taxonomies. These require evidence external to the codebase and were not equally rigorous.
- MOST RELIABLE: "Vikram implements X" — existence claims are easy to verify.
- MOST RISKY: "Vikram is better than Y at Z" — requires defining "better" and measuring both systems equally.

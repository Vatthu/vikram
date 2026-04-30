# Historical Context & Evolution

This document serves to persistently link the evolution of the **LeVik** project to past architectural discussions and prototyping efforts. Future agents working on this repository should use this context to understand why certain architectural decisions were made.

## Key Evolutionary Shifts

1. **NemoClaw & VikramAgent (Archived)**
   - Initially, the goal was to build an autonomous engineering team ("VikramAgent") inside a containerized setup using NemoClaw.
   - This was abandoned due to excessive security constraints and the difficulty of managing Docker-in-Docker workflows.
   - The surviving local `vikram_agent` workspace material is archived at `archive/prototypes/vikram_agent`. It contains identity and memory context, not the old Python implementation.

2. **OpenClaw Pivot (Conversation `fce26921-8094-47cc-be48-4641c713a728`)**
   - The team shifted to OpenClaw for runtime primitives, proposing a Lead-Worker agent model via a custom SOP orchestrator.
   - We eliminated legacy binaries and opted for a hybrid architecture: OpenClaw handling the execution runtime/Telegram bot, and Python handling the complex SOP phase gates.

3. **LeVik: v1claw Fork & Go-Native Engine (Conversation `19509686-f622-4ca7-930b-682b2e23b0cd`)**
   - Ultimately, maintaining a custom Python orchestrator on top of a heavy generic framework like OpenClaw was deemed suboptimal.
   - The decision was made to fork **v1claw** and learn from existing frameworks to build a specialized, production-ready system called **LeVik**.
   - **Architecture:** LeVik uses a Go native host layer (for gateway, permissions, workspace control, audit) decoupled entirely from OpenClaw, paired with a Python orchestration layer for workflow state and model routing.

## Linked Conversations for Context Retrieval
- **Architecting Autonomous Engineering Systems** (ID: `fce26921-8094-47cc-be48-4641c713a728`): Contains the blueprint for the SOP pipeline, Lead-Worker multi-agent hierarchy, and the realization that a specialized framework is needed.
- **Developing The Levik Agent System** (ID: `19509686-f622-4ca7-930b-682b2e23b0cd`): Details the final decoupling from OpenClaw and the birth of the Go-native LeVik architecture with its own SOP Loop and config standards.

*Agents should reference these logs if deep context on the "why" behind the Go/Python split or the Lead-Worker model is required.*

# MetaGPT Extraction

## Source Slice

- `metagpt/software_company.py`
- `metagpt/team.py`
- `metagpt/roles/role.py`
- `metagpt/environment/base_env.py`

## High-Signal Patterns

### 1. Team orchestration is explicit

MetaGPT does not hide collaboration in one agent. It makes the structure visible:

- `Team`
- `Environment`
- `Role`
- `Action`
- `Message`

That explicitness is valuable even if LeVik does not copy the same classes.

### 2. Roles have memory, state, and subscriptions

The `Role` base class has:

- message buffers
- memory
- working memory
- stage/state
- watched message types
- a planner

This is more useful than the “software company” marketing layer. The real signal is that roles are stateful workflow participants, not just renamed prompts.

### 3. Environment-mediated routing is the collaboration backbone

`Team` hires roles into an `Environment`, and the environment routes messages to recipients. That creates a structured collaboration plane.

### 4. Cost and budget are first-class

The `Team` tracks investment and checks budget during the run loop. That is directly relevant to LeVik, where model choice and autonomy should be budget-aware.

## What LeVik Should Borrow

- explicit logical roles
- role-local state and working memory
- artifact/message routing through a structured coordination layer
- budget-aware execution

## What LeVik Should Adapt

- Replace MetaGPT’s fixed org chart with stable logical roles and dynamic model assignment.
- Replace broad publish/subscribe chat with bounded artifact routing.
- Keep role state, but make artifacts the main contract between phases.

## What LeVik Should Reject

- literal “software company simulation” as the product architecture
- free-form multi-round role chatter as the default collaboration mode
- fixed vendor-to-role binding

## Concrete LeVik Impact

LeVik should define stable workflow roles such as:

- `planner`
- `implementer`
- `reviewer`
- `verifier`
- `integrator`

But each role should operate through artifacts and bounded messages, for example:

- `TaskSpec`
- `ImplementationPlan`
- `ChangeSet`
- `VerificationReport`
- `ReviewReport`
- `FounderDecision`

The orchestrator should route these artifacts deliberately instead of letting agents debate indefinitely.

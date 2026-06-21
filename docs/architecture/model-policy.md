# Model Policy

## Position

Vikram should not permanently bind a specific model vendor to a specific role.

The system will outlive today’s leaderboard. Roles should stay stable while model assignment changes as the ecosystem changes.

## Stable Concepts

Stable concepts:

- planner
- architect
- implementer
- test runner
- reviewer
- browser or visual verifier
- integrator

Unstable concepts:

- which provider is best for a role this month
- which model has the best browser or vision support
- which model gives the best quality-per-dollar

## V1 Policy

V1 uses a capability-based registry with explicit operator policy.

Each model entry should declare:

- provider
- model id
- reasoning strength
- coding strength
- review strength
- browser or vision support
- context length class
- latency class
- cost class
- reliability score

Role assignment is policy-driven:

- choose the cheapest model that satisfies minimum capability thresholds
- prefer observed reliability over hype
- allow manual operator overrides
- keep one fallback model per critical capability

## V2 Direction

In v2, the lead agent may propose team composition changes, but inside guardrails:

- budget limit
- allowed providers
- maximum worker count
- required approval for changing core review paths

The lead can suggest:

- swapping models for a role
- adding a temporary specialist
- downgrading low-value tasks to cheaper workers

The system should not allow unconstrained self-expansion.

## Practical View

Middle ground:

- do not hardcode model names to roles
- do not let the leader invent arbitrary teams without policy

That gives Vikram room to evolve without drifting into fantasy.

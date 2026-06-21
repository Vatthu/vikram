# Source Policy

Vikram will be built from clean, attributable inputs.

This research area exists to turn upstream code, official docs, and papers into design decisions. It does not exist to justify copying code from questionable sources.

## Allowed Sources

- official documentation
- public upstream repositories with clear licensing
- papers, benchmarks, and public technical reports
- small public repos that solve a narrow problem especially well

## Disallowed Sources

- leaked code
- repos or gists with unclear provenance
- "cleaned up" rewrites of leaked proprietary systems
- code copied from sources whose license or ownership cannot be established

## Why This Matters

- contaminated inputs create legal and product risk
- unclear provenance makes future open source or commercial licensing harder
- low-trust sources distort architecture decisions because they often optimize for imitation, not maintainability

## Practical Rule

If a source is interesting but provenance is unclear, we can study the public behavior it inspired and reproduce the idea cleanly from first principles. We do not import the code.

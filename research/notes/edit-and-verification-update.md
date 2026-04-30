# Edit And Verification Update

## Problem

After localization, LeVik still had a gap:

- it could identify likely change targets
- it could not yet prepare edits and verification in a disciplined way

That is the point where many agent systems either:

- jump into raw shell behavior
- overfit to chat loops
- or pretend the patching step is already solved

## Sources

- Agentless: `https://arxiv.org/abs/2407.01489`
- SWE-Search: `https://arxiv.org/abs/2410.20285`
- Unified Software Engineering Agent: `https://arxiv.org/abs/2506.14683`

## What These Sources Solve Well

### Agentless

Agentless reinforces the value of explicit stage boundaries:

- localization
- repair
- validation

For LeVik, the useful lesson is to keep repair and validation separate in artifacts and in runtime control.

### SWE-Search

SWE-Search argues that software agents improve when they can reconsider paths instead of following one rigid linear trajectory forever.

The relevant v1 lesson is smaller:

- keep the workflow explicit enough that future backtracking is possible
- do not bury all state inside one free-form conversation

### USEagent

USEagent pushes the idea of a unified software engineer that spans multiple SE tasks.

The practical lesson for LeVik is:

- preparation artifacts matter
- implementation and verification should be modeled as distinct competencies

## LeVik Decision

Borrow now:

- explicit implementation brief artifact
- explicit verification plan artifact
- bounded file writes as a host capability
- focused verification command discovery

Adapt later:

- backtracking when verification fails
- richer patch selection and retry logic
- deeper repair-policy evaluation

Reject for now:

- pretending that arbitrary shell execution is the edit model
- hiding validation inside the same step as editing
- adding search-heavy agent loops before the first edit-execute-review path is reliable

## Concrete Impact On LeVik

This note justifies the addition of:

- `POST /v1/files/write`
- `POST /v1/repos/discover-verification`

And the workflow transition:

- `implementation_ready -> change_ready`

Where `change_ready` means:

- targets localized
- file previews loaded
- implementation brief written
- verification candidates discovered
- verification artifact written

But not yet:

- patch applied
- verification executed

## Why This Matters

An enterprise-grade engineering team cannot blur planning, editing, and verification into one opaque step.

LeVik is moving toward:

- explicit preparation
- bounded edits
- focused verification
- recoverable execution state

# OpenClaw First Pass

## Source Snapshot

- local checkout: `research/upstream/openclaw`
- origin: `https://github.com/openclaw/openclaw`
- pinned `HEAD`: `6944d7025d9c0ba4891cbe646ab5dab039d504ca`

## What The Repo Actually Is

`openclaw` is a broad personal-assistant product, not an engineering-team framework. The local checkout includes:

- a large `docs/` tree
- extensive `cli/` documentation
- channel integrations
- app surfaces for macOS, iOS, and Android
- gateway and sandbox concepts

The upstream docs and repository shape confirm the same conclusion we already discussed: OpenClaw is strong on control-plane and operator-surface ideas, but it is not the architecture model for LeVik’s engineering team.

## What Problem It Solves Well

- channel routing
- approvals and operator UX
- session and gateway control
- packaging a usable AI system as a host-native product

## Why It Matters For LeVik

This is one of the best local references for the Go shell side of LeVik:

- Telegram as a serious operator surface
- control-plane oriented CLI/docs structure
- approval and channel patterns
- separation between gateway concerns and assistant behavior

## First Extraction Decision

- borrow control-plane and operator-experience ideas
- do not copy the personal-assistant product direction
- mine docs for gateway, approvals, routing, and security patterns

## Immediate Cautions

- OpenClaw assumes a broader personal assistant mission than LeVik.
- Its sandbox defaults lean toward Docker-based isolation, which does not fit LeVik’s host-native control-plane requirement.
- Much of the repo is irrelevant for LeVik v1, especially companion apps and broad consumer channel surfaces.

## Next Inspection Targets

1. `docs/cli/`
2. `docs/channels/telegram.md`
3. `docs/gateway/`
4. `docs/tools/`
5. `docs/automation/`

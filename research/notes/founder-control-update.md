# Founder Control Update

## Problem

Vikram could prepare and execute a bounded change attempt, but it still lacked a native founder-control loop:

- no explicit pause point for risky or failed changes
- no durable approval payload
- no clean resume path for founder decisions

Without that, Vikram would either:

- auto-complete changes too aggressively
- bury approval in ad hoc chat behavior
- or lose state between review and resume

## Sources

- LangGraph interrupts: `https://docs.langchain.com/oss/python/langgraph/interrupts`
- LangGraph persistence: `https://docs.langchain.com/oss/python/langgraph/persistence`

## What These Sources Solve Well

### Interrupts

LangGraph interrupts provide a durable pause point for human review.

The important behavior for Vikram is:

- a workflow can stop at a review gate
- the interrupt payload becomes the approval prompt
- the same thread can later resume with `Command(resume=...)`

### Persistence

Persistence makes the approval flow operationally real:

- the founder does not need to stay connected
- the orchestrator process can survive restarts
- the review decision can resume the same thread instead of reconstructing context

## Vikram Decision

Borrow now:

- interrupt-based founder gate for risky or failed change attempts
- explicit approval request artifact
- explicit founder decision artifact
- optional Telegram notification when an operator route is configured

Adapt later:

- richer decision payloads
- retry loops driven by founder feedback
- channel-specific approval UX

Reject for now:

- implicit approval through conversation state alone
- auto-merging risky code changes without a founder gate
- making Telegram a hard dependency for the approval model

## Concrete Impact On Vikram

This step justifies:

- `POST /v1/tasks/{task_id}/resume`
- approval request and founder decision artifacts
- interrupt-based pause/resume inside the LangGraph workflow

## Why This Matters

An enterprise-grade engineering team needs a durable operator boundary.

Vikram now has one for risky or failed change attempts.

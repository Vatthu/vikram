# Vikram Wiki

Welcome to the Vikram documentation. Vikram is an autonomous engineering team that runs on your infrastructure.

## Quick Navigation

| Page | What you'll learn |
|------|------------------|
| [Getting Started](Getting-Started) | Install, configure, and run your first task |
| [Configuration Guide](Configuration-Guide) | Agent roster, providers, budget, and approval settings |
| [Submitting Tasks](Submitting-Tasks) | How to give Vikram work via CLI, Telegram, or API |
| [Understanding the Workflow](Understanding-the-Workflow) | What happens step-by-step when a task runs |
| [Approval & Governance](Approval-and-Governance) | How to control what auto-approves vs. what needs you |
| [Cost & Budget](Cost-and-Budget) | Spending limits, budget strategy, and cost tracking |
| [Team Formations](Team-Formations) | Configuring which models handle which roles |
| [Telegram Setup](Telegram-Setup) | Setting up mobile approvals via Telegram |
| [Troubleshooting](Troubleshooting) | Common problems and how to fix them |

## What is Vikram?

You give it an engineering objective. It:
1. Plans the approach
2. Implements in an isolated branch
3. Verifies with tests and property checking
4. Asks for your approval (or auto-approves if trusted)
5. Delivers a merge-ready branch

All on your machine, with your API keys.

## Requirements

- Mac or Linux (or Windows with WSL)
- Go 1.22+ and Python 3.12+
- At least one LLM API key
- A git repository to work on
- Your project's build/test toolchain installed

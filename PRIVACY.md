# Privacy & Security

Vikram is built on a fundamental principle: **your data never leaves your machine unless you explicitly tell it to.**

## Zero Cloud Dependency

Unlike cloud-based AI assistants that route your code, conversations, and files through third-party servers, Vikram runs entirely on your local machine:

- **No proxy servers** — your API keys go directly to the LLM provider you choose
- **No telemetry** — zero analytics, tracking, or usage reporting
- **No phone-home** — Vikram never contacts any Vikram-operated server
- **No data collection** — we don't see, store, or process any of your data

## Your Keys, Your Control

Vikram supports 20+ LLM providers. You bring your own API keys, which are:

- Stored locally in `~/.vikram/config.json` (file permissions: `0600`)
- Never transmitted to any endpoint other than the provider's official API
- Never logged, cached, or written to any other location
- Replaceable with environment variables for additional security

## Sandboxed Execution

By default, Vikram operates with strict security boundaries:

- **Workspace restriction** — file operations are sandboxed to your configured workspace
- **Shell permissions** — configurable allow/deny lists for shell commands
- **Tool approval** — dangerous operations require explicit configuration
- **Hardware permissions** — camera, microphone, SMS, and other hardware access is blocked by default and requires explicit opt-in

## Data at Rest

All persistent data stays on your filesystem:

| Data | Location | Purpose |
|------|----------|---------|
| Configuration | `~/.vikram/config.json` | Settings and API keys |
| Sessions | `~/.vikram/workspace/sessions/` | Conversation history |
| Memory | `~/.vikram/workspace/memory/` | Long-term knowledge |
| Epistemology | `~/.vikram/workspace/epistemology.db` | Knowledge graph (SQLite) |
| Logs | `~/.vikram/workspace/logs/` | Diagnostic logs |

All files use restrictive permissions (`0600`/`0700`). No data is synced to any cloud service.

## Network Activity

Vikram only makes network requests when:

1. **You send a message** → API call to your configured LLM provider
2. **You use web tools** → search queries to your configured search provider
3. **You enable channels** → connections to Telegram/WhatsApp APIs (opt-in)

That's it. No background network activity, no periodic check-ins, no update pings.

## Comparison with Alternatives

| Feature | Vikram | Cloud-based AI IDEs | Hosted AI Agents |
|---------|--------|--------------------|--------------------|
| Code stays local | ✅ | ❌ | ❌ |
| Your own API keys | ✅ | ❌ | Partial |
| No telemetry | ✅ | ❌ | ❌ |
| No account required | ✅ | ❌ | ❌ |
| Offline capable* | ✅ | ❌ | ❌ |
| Open source | ✅ | Partial | Partial |
| Self-hosted | ✅ | ❌ | ❌ |

*With local LLM providers like Ollama or vLLM.

## Responsible Disclosure

If you discover a security vulnerability, please report it privately:

- **GitHub**: [Report a vulnerability](https://github.com/Vatthu/vikram/security/advisories/new)
- **Email**: amit.vikramaditya@icloud.com

Do not open public issues for security vulnerabilities. We aim to acknowledge reports within 48 hours and provide a fix within 7 days for critical issues.

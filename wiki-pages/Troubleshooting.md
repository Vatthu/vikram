# Troubleshooting

## Common Issues

### "vikram: command not found"

The binary isn't in your PATH. Either:
- Run `make install` to install to `~/.local/bin/`
- Or use the full path: `./build/vikram-darwin-arm64 doctor`

### "Config file not found"

Run `vikram onboard` to create your initial configuration.

### "LLM connectivity: FAIL"

Your API key is invalid or the provider is unreachable.
- Check your key in `~/.vikram/config.json`
- Test directly: `curl https://api.anthropic.com/v1/messages -H "x-api-key: YOUR_KEY"`
- If using Ollama: make sure `ollama serve` is running

### "Orchestrator not reachable"

The Python orchestrator isn't running. Start it:
```bash
cd services/orchestrator
source .venv/bin/activate
python -m vikram_orchestrator.main
```

Or start both together:
```bash
vikram gateway
```

### Task fails at "discover_team" with "team_unavailable"

The orchestrator couldn't connect to the Go host. Check:
- Is `vikram gateway` running?
- Is the Unix socket at `/tmp/vikramd.sock` accessible?

### Task hangs at implementation phase

The engineer agent is producing output that can't be parsed.
- Check task artifacts for the raw agent response
- The system retries once with a corrective prompt
- If it still fails, the task halts and notifies you

### Budget exceeded — task paused

Your task hit its `max_cost_usd` limit.
- Resume with increased budget via Console or API
- Or cancel the task

### Tests fail but I know the code is correct

Vikram runs whatever test command your project has. If tests are flaky:
- Check your test suite locally: does it pass consistently?
- Vikram can't distinguish flaky tests from real failures

### Telegram bot doesn't respond

- Is `vikram gateway` running?
- Check your bot token: `vikram doctor` validates it
- Check your user ID is in `allow_from`
- Make sure you're messaging the bot directly (not in a group)

### Merge conflicts

If the task branch can't merge cleanly:
- Another task or manual commit changed the same files
- Vikram will report the conflict in the merge assessment
- Rebase the task branch or cancel and re-submit

## Logs

### Go host logs
The gateway prints logs to stdout. Use `--debug` for verbose output:
```bash
vikram gateway --debug
```

### Python orchestrator logs
Located at stdout of the orchestrator process. Includes all workflow transitions and agent calls.

### Task artifacts
Each task's plan, implementation, verification, and review artifacts are stored at:
```
~/.vikram/workspace/tasks/{task_id}/artifacts/
```

## Getting Help

- [GitHub Discussions](https://github.com/Vatthu/vikram/discussions) — ask questions, share setups
- [GitHub Issues](https://github.com/Vatthu/vikram/issues) — report bugs
- [FAQ](https://github.com/Vatthu/vikram/blob/main/FAQ.md) — quick answers to common questions

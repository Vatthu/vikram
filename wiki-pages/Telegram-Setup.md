# Telegram Setup

Telegram is the recommended channel for mobile approvals. You get notified when tasks need review, and can approve/reject from your phone.

## Create a Telegram Bot

1. Open Telegram and message [@BotFather](https://t.me/BotFather)
2. Send `/newbot`
3. Choose a name (e.g., "My Vikram")
4. Choose a username (e.g., `my_vikram_bot`)
5. BotFather gives you a token like `123456789:ABCdefGHIjklMNOpqrSTUvwxYZ`

## Get Your User ID

1. Message [@userinfobot](https://t.me/userinfobot) on Telegram
2. It replies with your numeric user ID (e.g., `987654321`)

## Configure Vikram

Add to your `~/.vikram/config.json`:

```json
{
  "channels": {
    "telegram": {
      "enabled": true,
      "token": "123456789:ABCdefGHIjklMNOpqrSTUvwxYZ",
      "allow_from": ["987654321"]
    }
  }
}
```

The `allow_from` field is critical — it restricts who can interact with your bot. Without it, anyone who discovers your bot could submit tasks.

## Start the Gateway

```bash
vikram gateway
```

The gateway connects to Telegram and starts listening for messages.

## First Message

Send any message to your bot. You should get a response. If not, check:
- Is the gateway running?
- Is your user ID correct in `allow_from`?
- Is the bot token valid? (`vikram doctor` will test this)

## What You Can Do via Telegram

- **Submit tasks**: Just type your objective as a message
- **Receive notifications**: Phase transitions, approval requests, budget warnings
- **Approve/reject**: When asked, reply with your decision
- **Check status**: Ask "what's running?" or "task status"

## Security Notes

- Always set `allow_from` to your user ID only
- Don't share your bot token
- The bot only responds to users in the allowlist
- If you suspect compromise, revoke the token via @BotFather (`/revoke`)

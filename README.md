[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

# Telegram MCP server

> [!NOTE]
> This is a personal fork of [chaindead/telegram-mcp](https://github.com/chaindead/telegram-mcp). All credit for the original work goes to its author, [chaindead](https://github.com/chaindead). Distributed under the original MIT license (see [LICENSE](LICENSE)).

The server is a bridge between the Telegram API and AI assistants and is based on the [Model Context Protocol](https://modelcontextprotocol.io). It talks only to Telegram, has no telemetry, and runs nothing else on your computer.

> [!IMPORTANT]
> Ensure that you have read and understood the [Telegram API Terms of Service](https://core.telegram.org/api/terms) before using this server.
> Any misuse of the Telegram API may result in the suspension of your account.

## Table of Contents
- [What does this server do?](#what-does-this-server-do)
- [Setup (Windows)](#setup-windows)
  - [1. Build](#1-build)
  - [2. Add credentials](#2-add-credentials)
  - [3. Log in to Telegram](#3-log-in-to-telegram)
  - [4. Add to Claude Desktop](#4-add-to-claude-desktop)
- [Security notes](#security-notes)
- [Optional: JSON Schema Version](#optional-json-schema-version)

## What does this server do?

Tools exposed to the AI assistant:

- `tg_me` — get current account information
- `tg_dialogs` — list dialogs, optionally only unread ones
- `tg_dialog` — read messages from a specific dialog
- `tg_send` — save a **draft** message in a dialog (it never sends anything)
- `tg_read` — mark a dialog as read

Example prompts:
- "Summarize all my unread Telegram messages"
- "Draft a polite response to the last message in [chat]"
- "Check if there are any unanswered questions in my chats"

## Setup (Windows)

Requirements: [Go](https://go.dev/dl/) 1.24+ and [Claude Desktop](https://claude.ai/download).

### 1. Build

```powershell
git clone https://github.com/dmitriy-fullstack-2010/telegram-mcp.git C:\Projects\telegram-mcp
cd C:\Projects\telegram-mcp
go build -o telegram-mcp.exe .
```

### 2. Add credentials

Get an API ID and API hash at [my.telegram.org](https://my.telegram.org/auth) (API development tools), then create `%USERPROFILE%\.telegram-mcp\.env`:

```dotenv
TG_APP_ID=<your-api-id>
TG_API_HASH=<your-api-hash>
```

Credentials live only in this file, not in the Claude config. Keep it private and never commit it. To use another location, set `TG_ENV_FILE=<path>`. Variables already set in the real environment take priority over the file.

### 3. Log in to Telegram

Run once:

```powershell
C:\Projects\telegram-mcp\telegram-mcp.exe auth --phone +<your-phone-number>
```

Enter the code Telegram sends to your Telegram app. Add `--password <2fa-password>` if you have two-step verification, or `--new` to replace an existing session. This creates `%USERPROFILE%\.telegram-mcp\session.json`.

### 4. Add to Claude Desktop

Open `%APPDATA%\Claude\claude_desktop_config.json` and add:

```json
{
  "mcpServers": {
    "telegram": {
      "command": "C:/Projects/telegram-mcp/telegram-mcp.exe"
    }
  }
}
```

If the file already has `mcpServers`, add only the `"telegram"` entry inside it. Then fully quit and reopen Claude Desktop and ask, for example, "Who am I on Telegram?".

## Security notes

- `session.json` is a full login to your Telegram account. Anyone who gets the file has access without a password or code. You can end the session in Telegram → Settings → Devices.
- The AI can read all your chats, save drafts and mark chats as read. It cannot send messages.
- Build from source yourself; do not use prebuilt binaries you did not audit.

## Optional: JSON Schema Version

Some MCP clients (e.g. VS Code) do not support JSON Schema Draft 2020-12 and will reject tools that use it. Override it with the `--schema-version` flag or the `TG_SCHEMA_VERSION` variable (for example in the `.env` file).

| Version | URL |
|---------|-----|
| Draft-07 (recommended for VS Code) | `https://json-schema.org/draft-07/schema#` |
| Draft 2020-12 (default) | `https://json-schema.org/draft/2020-12/schema` |

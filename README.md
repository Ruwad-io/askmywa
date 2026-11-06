# WhatsApp MCP

*[Version française](README.fr.md)*

An [MCP](https://modelcontextprotocol.io) server for your personal WhatsApp account, as a single Go binary for Mac, Windows and Linux, including Raspberry Pi.

It links to WhatsApp as a companion device through [whatsmeow](https://github.com/tulir/whatsmeow), keeps a local archive of your messages, and lets an assistant such as Claude Desktop search them, read them, download attachments and, after your approval, send messages and files.

![WhatsApp MCP in Claude Desktop](./example-use.png)

## What it does

- **Local archive.** Every message lands in a SQLite database on your computer. Nothing reaches the assistant unless it calls a tool.
- **Real names.** Senders are resolved when messages are stored, from your address book first, then the name people set for themselves. Phone numbers and WhatsApp's hidden `@lid` identifiers are merged into one contact.
- **Full-text search.** Accent- and case-insensitive: `ecole` finds `école`.
- **Typed tools.** Every tool declares its input and output schema, paginates with a cursor and returns errors that say how to fix the call.
- **Sending needs your approval.** `send_message`, `send_file` and `send_audio_message` only return a preview (recipient, resolved name, group or not, content). They send only when called again with `confirm=true`.
- **No network port.** The MCP server talks to the WhatsApp daemon over a private Unix socket. Files can only be sent from one configured folder.

> **Caution.** Like any MCP server that reads private data and can send messages, this one is exposed to [the lethal trifecta](https://simonwillison.net/2025/Jun/16/the-lethal-trifecta/): a prompt injection hidden in a message could try to make the assistant leak data. The confirmation step on every send is the main safeguard, so read previews before approving them.

## How it differs from lharries/whatsapp-mcp

This is a rewrite, not a patch set. The tool names and the WhatsApp library are the same, so assistants and prompts written for the original keep working.

| | [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) | This project |
|---|---|---|
| Components | Go bridge plus a Python MCP server run with `uv` | One Go binary |
| Link between them | HTTP API on port 8080 of every network interface, no authentication | Private Unix socket, readable only by you |
| Sender names | Looked up at read time from the chat list; group members and `@lid` senders often show as raw numbers | Resolved when stored, from the address book first; phone numbers and `@lid` identities merged into one contact |
| Search | `LIKE` on message text, accent-sensitive, unindexed | SQLite FTS5 index, accent- and case-insensitive |
| Tool results | Mostly formatted text, page numbers | Typed JSON with schemas, cursor pagination with `has_more` |
| Sending | Immediate | Preview first, sent only with `confirm=true` |
| Files that can be sent | Any readable path | Only files under one configured folder |
| Running it | `go run main.go` in an open terminal | A background service with rotated logs on Mac, Windows and Linux |
| Installing it | Go, Python, `uv`, a terminal QR code and a hand-edited JSON config | A Claude Desktop extension, linked with a code typed on the phone |

An existing install of the original can switch without pairing again: the new daemon copies the old session and imports the old history, and never writes to the old files. See [Migrating from the original project](whatsapp-go/README.md#migrating-from-the-original-project).

## Install in Claude Desktop (Mac and Windows, no terminal)

1. Download `whatsapp-mcp-<version>.mcpb` from the [latest release](https://github.com/abonur/whatsapp-mcp/releases/latest). The same file works on Mac and Windows.
2. Double-click it. Claude Desktop opens and asks you to confirm the install.
3. In a new conversation, write: **Connect my WhatsApp.**
4. Claude asks for your phone number, then gives you an 8-character code. On your phone, open WhatsApp > Settings (on Android, the ⋮ menu) > Linked devices > Link a device > **Link with phone number instead**, and type the code.

That's it. Your messages start appearing within a few minutes, and the link keeps working after a restart. To send a file, put it in the `Documents/WhatsApp MCP` folder first (you can choose another folder in the extension's settings).

To remove it, ask Claude to **disconnect WhatsApp from this computer** (it unlinks the computer, stops the background service and, if you want, deletes the stored messages), then uninstall the extension in Claude Desktop's settings.

> The Mac app is signed and notarized by Apple. The Windows app is not code-signed yet: if Windows shows a SmartScreen warning, choose *More info* > *Run anyway*.
>
> It talks to WhatsApp through an unofficial interface, the same way WhatsApp Web automation tools do. WhatsApp's terms do not allow this, and WhatsApp could restrict an account that uses it.

## Install without the extension (Mac, Windows, Linux)

Download the archive for your system from the [latest release](https://github.com/abonur/whatsapp-mcp/releases/latest), or build from source with Go 1.26 or newer:

| System | Archive |
|---|---|
| Mac (Apple Silicon / Intel) | `darwin-arm64` / `darwin-amd64` |
| Windows (x64 / ARM) | `windows-amd64` / `windows-arm64` |
| Linux, Raspberry Pi 4 and 5 with a 64-bit OS | `linux-amd64` / `linux-arm64` |
| Raspberry Pi with a 32-bit OS (Pi 2, 3, Zero 2) | `linux-armv7` |

```bash
git clone https://github.com/abonur/whatsapp-mcp.git
cd whatsapp-mcp/whatsapp-go
mkdir -p ~/.local/bin
go build -o ~/.local/bin/whatsapp-mcp .
```

Install the background service, which starts at login (a launchd agent on Mac, a systemd user service on Linux, a scheduled task on Windows):

```bash
~/.local/bin/whatsapp-mcp install-service
```

Then link WhatsApp, either by asking Claude to connect it as in the extension steps above, or in a terminal: uninstall the service, run `whatsapp-mcp daemon`, scan the QR code from WhatsApp (Linked devices > Link a device), stop it with Ctrl+C and install the service again.

Add the server to Claude Desktop's `claude_desktop_config.json` (Mac: `~/Library/Application Support/Claude/`, Windows: `%APPDATA%\Claude\`) and restart Claude Desktop. The path must be absolute: `~` is not expanded there.

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "/Users/YOU/.local/bin/whatsapp-mcp",
      "args": ["serve"]
    }
  }
}
```

On Windows, use the full path to `whatsapp-mcp.exe`, with doubled backslashes. Cursor uses the same block in `~/.cursor/mcp.json`, and Claude Code accepts it with `claude mcp add whatsapp -- /path/to/whatsapp-mcp serve`.

### On a Raspberry Pi or another always-on machine

A Pi left on keeps the archive complete even when your laptop sleeps. Install the Linux binary on the Pi, run `whatsapp-mcp install-service` there (it asks systemd to keep the service running after you log out), and link WhatsApp once over SSH with `whatsapp-mcp daemon` and its QR code.

Claude Desktop on your laptop can then use the Pi over SSH, provided you log in to the Pi with an SSH key, without a password prompt:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "ssh",
      "args": ["pi@raspberrypi.local", "/home/pi/.local/bin/whatsapp-mcp", "serve"]
    }
  }
}
```

Files to send must then be on the Pi, in its send folder.

## Tools

| Tool | What it does |
|---|---|
| `whatsapp_status` | Whether WhatsApp is set up and connected, and what to do next |
| `connect_whatsapp` | Start the background service and link this computer with a pairing code |
| `disconnect_whatsapp` | Unlink this computer, stop the service and optionally delete the archive, after a preview |
| `search_contacts` | Find people by name or number |
| `list_chats` | Chats, most recent first or by name |
| `list_messages` | Messages filtered by chat, sender, dates and full-text query, with optional surrounding context |
| `get_chat`, `get_direct_chat_by_contact`, `get_contact_chats` | One chat, the direct chat with someone, every chat they appear in |
| `get_last_interaction`, `get_message_context` | Latest exchange with someone, messages around a given one |
| `download_media` | Save an image, video, audio, document or sticker and return its path |
| `send_message`, `send_file`, `send_audio_message` | Preview by default, send with `confirm=true` |

Contacts can be given as a phone number, a JID or a name.

## More

[`whatsapp-go/README.md`](whatsapp-go/README.md) covers the data layout, migration from the original project, service management, configuration and development.

## Credits

Started as a fork of [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp) by Luke Harries, MIT licensed. The voice-note encoding and the overall tool set come from that project; its Go bridge and Python server were removed once this binary replaced them, and remain in this repository's git history.

# WhatsApp MCP

An [MCP](https://modelcontextprotocol.io) server for your personal WhatsApp account, as a single Go binary.

It links to WhatsApp as a companion device through [whatsmeow](https://github.com/tulir/whatsmeow), keeps a local archive of your messages, and lets an assistant such as Claude Desktop search them, read them, download attachments and, after your approval, send messages and files.

![WhatsApp MCP in Claude Desktop](./example-use.png)

## What it does

- **Local archive.** Every message lands in a SQLite database on your Mac. Nothing reaches the assistant unless it calls a tool.
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
| Running it | `go run main.go` in an open terminal | launchd service with rotated logs |

An existing install of the original can switch without pairing again: the new daemon copies the old session and imports the old history, and never writes to the old files. See [Migrating from the original project](whatsapp-go/README.md#migrating-from-the-original-project).

## Quick start (macOS)

Requires Go 1.26 or newer. `ffmpeg` is optional and only used to turn audio files into voice notes.

```bash
git clone https://github.com/abonur/whatsapp-mcp.git
cd whatsapp-mcp/whatsapp-go
mkdir -p ~/.local/bin
go build -o ~/.local/bin/whatsapp-mcp .
```

Pair once, in a terminal, and scan the QR code from WhatsApp (Linked devices > Link a device):

```bash
~/.local/bin/whatsapp-mcp daemon
```

Once it logs `connected to WhatsApp`, stop it with Ctrl+C and install it as a background service that starts at login:

```bash
~/.local/bin/whatsapp-mcp install-service
```

Then add the server to `~/Library/Application Support/Claude/claude_desktop_config.json` and restart Claude Desktop. The path must be absolute: `~` is not expanded there.

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

Cursor uses the same block in `~/.cursor/mcp.json`.

## Tools

| Tool | What it does |
|---|---|
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

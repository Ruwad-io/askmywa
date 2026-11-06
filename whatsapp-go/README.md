# whatsapp-mcp (Go)

One Go binary that replaces the previous `whatsapp-bridge` (Go) + `whatsapp-mcp-server` (Python) pair:

- `whatsapp-mcp daemon` connects to WhatsApp with [whatsmeow](https://github.com/tulir/whatsmeow), archives every message in a clean SQLite database and serves sends and downloads over a Unix socket.
- `whatsapp-mcp serve` is an MCP server over stdio, built on the official [`modelcontextprotocol/go-sdk`](https://github.com/modelcontextprotocol/go-sdk), with the same tools as the Python server.
- `whatsapp-mcp install-service` / `uninstall-service` run the daemon as a launchd agent.

Everything lives in `~/Library/Application Support/whatsapp-mcp/`:

| File | Role |
|---|---|
| `whatsapp.db` | whatsmeow session (a copy of the old one, never the original) |
| `messages.db` | contacts, chats and messages; FTS5 index on message text |
| `daemon.sock` | Unix socket (mode 0600) used by `serve` to send and download |
| `files/` | the only directory files can be sent from; downloads land in `files/media/<chat>/` |

What changed compared with the old pair:

- Senders are stored as a `(phone, lid)` pair, and the sender's name is resolved when the row is written (address-book name > first name > business name > push name), then refreshed when WhatsApp sends a better name. Readers never touch whatsmeow's tables.
- Full-text search with FTS5 (`unicode61`, `remove_diacritics 2`): `ecole` finds `école`.
- Typed tool inputs and outputs with JSON schemas, cursor pagination with `has_more`, errors that say how to fix the call.
- The three send tools preview by default and only send with `confirm=true`.
- No TCP port: the MCP server talks to the daemon through the socket.
- Files can only be sent from the send root (symlinks resolved).

## Build

Go 1.26 or newer. No cgo: SQLite comes from `modernc.org/sqlite`.

```bash
cd whatsapp-go
go build -o whatsapp-mcp .
```

Optional: `brew install ffmpeg` for `send_audio_message` (conversion to Opus voice notes).

Put the binary somewhere stable, for example `/usr/local/bin/whatsapp-mcp`; the launchd agent records its absolute path.

## Migrating from the old bridge

The daemon never opens the old `whatsapp-bridge/store/` for writing. On first run it copies `whatsapp.db` (session) with `VACUUM INTO` and imports `messages.db` (history), then connects with the same linked device: no new QR code, no extra linked-device slot.

Only one process may use the session at a time, so stop the old bridge first:

```bash
../whatsapp-bridge/install-service.sh uninstall
```

Then either import without connecting (safe to run while the old bridge is still up, it only reads):

```bash
./whatsapp-mcp import --legacy-store /path/to/whatsapp-mcp/whatsapp-bridge/store
```

or start the daemon directly with the same flag; it imports when the archive is empty:

```bash
./whatsapp-mcp daemon --legacy-store /path/to/whatsapp-mcp/whatsapp-bridge/store
```

Check the result:

```bash
sqlite3 "$HOME/Library/Application Support/whatsapp-mcp/messages.db" "select name, phone, lid from contacts where name_fold like '%hani%'"
```

## First pairing (no previous session)

```bash
./whatsapp-mcp daemon
```

A QR code is printed; scan it from WhatsApp (Linked devices > Link a device). The session is saved in `whatsapp.db` and reused afterwards. Logs go to stderr (`--log-file FILE` writes rotated logs instead; `--debug` is verbose).

## Install as a service

```bash
./whatsapp-mcp install-service
```

This writes `~/Library/LaunchAgents/com.whatsapp-mcp.daemon.plist` (RunAtLoad, KeepAlive, restart after 10 s) and starts the daemon with `--log-file ~/Library/Logs/whatsapp-mcp/daemon.log` (rotated at 10 MB, 5 files kept, compressed). Pass `--legacy-store DIR` to migrate on first start and `--send-root DIR` to change where files may be sent from.

```bash
./whatsapp-mcp status            # launchd state, connection, row counts
./whatsapp-mcp uninstall-service
```

The agent must be installed from the final location of the binary: reinstall after moving it.

## Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "/usr/local/bin/whatsapp-mcp",
      "args": ["serve"]
    }
  }
}
```

`serve` opens `messages.db` read-only and needs the daemon only for `send_*` and `download_media`. Environment overrides: `WHATSAPP_MCP_DATA_DIR`, `WHATSAPP_MCP_SEND_ROOT`.

## Tools

| Tool | Notes |
|---|---|
| `search_contacts` | name (accent-insensitive) or number; returns the JID to use elsewhere |
| `list_messages` | filters: `chat_jid`, `sender` (number, JID or name), `query` (FTS), `after`, `before`; `include_context`; cursor pagination |
| `list_chats`, `get_chat`, `get_direct_chat_by_contact`, `get_contact_chats`, `get_last_interaction`, `get_message_context` | read-only |
| `send_message`, `send_file`, `send_audio_message` | without `confirm` they return a preview (resolved JID, name, group or not, content) and send nothing; with `confirm=true` they send through the daemon |
| `download_media` | writes under `files/media/<chat>/`, returns the path |

Read tools carry `readOnlyHint`; send tools carry `destructiveHint` and `openWorldHint`.

## Development

```bash
go vet ./... && go test ./...
./scripts/whatsmeow-parity.sh   # every whatsmeow method we call still exists
```

CI (`.github/workflows/whatsapp-go.yml`) runs gofmt, vet, tests with `-race`, the build on Linux and macOS, and the parity script. A weekly job upgrades whatsmeow to `@main`, builds and tests, and reports in a rolling issue.

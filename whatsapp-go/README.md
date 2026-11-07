# whatsapp-mcp

One binary, six commands:

| Command | Role |
|---|---|
| `whatsapp-mcp daemon` | Stays connected to WhatsApp, archives every message, serves sends and downloads over a Unix socket |
| `whatsapp-mcp serve` | MCP server over stdio, started by Claude Desktop or Cursor |
| `whatsapp-mcp install-service` / `uninstall-service` | Run the daemon as a background service (launchd, systemd or a Windows scheduled task) |
| `whatsapp-mcp link` | Link WhatsApp to the running service with a code typed on the phone |
| `whatsapp-mcp import` | Import the session and history of an lharries/whatsapp-mcp install without connecting |
| `whatsapp-mcp status` | Service state, connection and archive size |

Built on [whatsmeow](https://github.com/tulir/whatsmeow), the official [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) and [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (pure Go, no cgo).

## Install

The simplest install is the Claude Desktop extension (Mac and Windows): download [`whatsapp-mcp.mcpb`](https://github.com/abonur/whatsapp-mcp/releases/latest/download/whatsapp-mcp.mcpb) (also on [askmywa.com](https://askmywa.com)), double-click it, then ask Claude to connect WhatsApp (see [Pairing from Claude](#pairing-from-claude)). Prebuilt binaries for macOS, Windows (x64, ARM), Linux (x64, ARM64) and 32-bit Raspberry Pi kernels (`armv7`) are attached to the same releases.

On Linux, a Raspberry Pi or a Mac, the installer picks the right archive, checks it against the release's `checksums.txt`, installs it in `~/.local/bin`, installs the service and offers to link WhatsApp. Run it again to upgrade:

```bash
curl -fsSL https://github.com/abonur/whatsapp-mcp/releases/latest/download/install.sh | sh
# options: sh -s -- --no-service | --no-link | --dir DIR | --version X.Y.Z
```

With Homebrew: `brew install abonur/tap/whatsapp-mcp`. The service then runs the formula's `opt` path, so it keeps working after `brew upgrade`; run `install-service` again after an upgrade to restart it on the new version.

To build from source you need Go 1.26 or newer. Optional: `brew install ffmpeg`, used by `send_audio_message` to convert audio to Opus voice notes.

```bash
git clone https://github.com/abonur/whatsapp-mcp.git
cd whatsapp-mcp/whatsapp-go
mkdir -p ~/.local/bin
go build -o ~/.local/bin/whatsapp-mcp .
```

The commands below assume `~/.local/bin` is on your `PATH`; otherwise call the binary by its full path.

Build to a stable location you can write to without `sudo` (`/usr/local/bin` usually needs it). The launchd agent and the Claude Desktop config both record the binary's absolute path, so a binary inside a temporary folder or a git worktree breaks them when that folder goes away.

## First pairing

There are three ways to link the daemon to your WhatsApp account. Each way, the session is saved and reused on every later start.

### Pairing from Claude

With the service installed (or with the extension, which installs it on first use), ask Claude to connect WhatsApp. The `connect_whatsapp` tool starts the service if needed, asks WhatsApp for an 8-character code for your phone number, and Claude shows it to you. On the phone: WhatsApp > Settings > Linked devices > Link a device > **Link with phone number instead**, then type the code within about two minutes. `whatsapp_status` reports the result.

A daemon without a session that is not attached to a terminal (the service) waits for this request and does not connect to WhatsApp until then.

### Pairing in a terminal, with a code

With the service running, for example on a Raspberry Pi over SSH:

```bash
whatsapp-mcp link
```

It asks for the phone number (or takes `--phone`), prints the 8-character code and waits until the phone has entered it. This is the same request as `connect_whatsapp`, so it needs no screen and no QR code.

### Pairing in a terminal, with a QR code

Without the service running:

```bash
whatsapp-mcp daemon
```

A QR code appears in the terminal. Scan it from WhatsApp (Linked devices > Link a device). Stop with Ctrl+C once the log shows `connected to WhatsApp`. `--pair qr` or `--pair wait` forces either behaviour.

## Run as a service

```bash
whatsapp-mcp install-service
whatsapp-mcp status
```

The service starts at login, restarts after a crash, and logs to `daemon.log` in the log folder (see [Files](#files)), rotated at 10 MB with 5 compressed files kept.

| System | What `install-service` registers |
|---|---|
| macOS | launchd agent `~/Library/LaunchAgents/com.whatsapp-mcp.daemon.plist`, restarted 10 s after a crash |
| Linux, Raspberry Pi | systemd user service `~/.config/systemd/user/whatsapp-mcp.service`, `Restart=always`. It also enables lingering (`loginctl enable-linger`) so the service runs at boot and after you log out; if that needs `sudo`, it says so |
| Windows | scheduled task `whatsapp-mcp daemon`, started at logon and restarted every minute after a failure. The console window closes by itself right after it opens |

`install-service --dry-run` prints the definition without installing anything, and `--no-wait` returns as soon as the service is started.

```bash
whatsapp-mcp uninstall-service
```

After rebuilding or moving the binary, run `install-service` again from its new location.

`install-service` waits until the daemon is connected and prints the account, or tells you why it is not.

Only one process may use a WhatsApp session at a time. Never run `whatsapp-mcp daemon` by hand while the service is loaded, and never run the original project's bridge alongside it: WhatsApp disconnects both.

## Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json`:

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

Use the absolute path: `~` is not expanded in this file. Restart Claude Desktop afterwards (Cmd+Q, then reopen). `serve` reads the archive directly, so reading works even when the daemon is down. Sending and downloading need the daemon.

## Migrating from the original project

If you already run [lharries/whatsapp-mcp](https://github.com/lharries/whatsapp-mcp), this binary can take over its WhatsApp session, so no new QR code and no extra linked-device slot are needed. It copies the session file and imports the message history from the original's `whatsapp-bridge/store/` folder, which it only reads and never modifies.

1. Stop the original bridge: press Ctrl+C in the terminal running `go run main.go`, or unload the launchd agent if you made one.

2. Start the daemon once with the **absolute** path of the original store, inside your clone of the original project:

   ```bash
   whatsapp-mcp daemon --legacy-store /path/to/lharries-whatsapp-mcp/whatsapp-bridge/store
   ```

   The log should show `legacy session copied`, `legacy history imported`, then `connected to WhatsApp`. If a QR code appears instead, stop with Ctrl+C without scanning it: the path is wrong. The daemon refuses to start when the folder has no `whatsapp.db`.

3. Stop it with Ctrl+C and install the service as above.

4. Point Claude Desktop at `whatsapp-mcp serve` instead of `uv run main.py`.

`whatsapp-mcp import --legacy-store DIR` does the copy and import of step 2 without connecting, which is safe while the original bridge still runs. Run it again right before switching: the original bridge keeps rotating its encryption keys, so an old copy goes stale. To start over, delete `~/Library/Application Support/whatsapp-mcp/` (with the service uninstalled).

## Files

Everything lives in one data folder (override with `--data-dir` or `WHATSAPP_MCP_DATA_DIR`), and logs in a separate log folder:

| System | Data folder | Log folder |
|---|---|---|
| macOS | `~/Library/Application Support/whatsapp-mcp` | `~/Library/Logs/whatsapp-mcp` |
| Windows | `%LOCALAPPDATA%\whatsapp-mcp` | `%LOCALAPPDATA%\whatsapp-mcp\logs` |
| Linux | `~/.local/share/whatsapp-mcp` (`$XDG_DATA_HOME`) | `~/.local/state/whatsapp-mcp` (`$XDG_STATE_HOME`) |

Inside the data folder:

| Path | Content |
|---|---|
| `whatsapp.db` | WhatsApp session keys. Keep it private; deleting it means pairing again |
| `messages.db` | Contacts, chats and messages, with a full-text index |
| `daemon.sock` | Socket used by `serve`, readable only by you (a Unix socket, also on Windows 10 and 11) |
| `files/` | The only folder files can be sent from (`--send-root` or `WHATSAPP_MCP_SEND_ROOT` to change it) |
| `files/media/<chat>/` | Downloaded attachments |

### Archive schema

- `contacts(phone, lid, name, name_source)`: one row per person. `phone` and `lid` are the bare digits of the `@s.whatsapp.net` and `@lid` identifiers, merged when WhatsApp reveals they belong together. `name_source` is `full_name`, `first_name`, `business_name`, `push_name` or `chat`, best first; a worse source never overwrites a better one.
- `chats(jid, name, is_group, last_message_time)`.
- `messages(id, chat_jid, sender_phone, sender_lid, sender_name, content, timestamp, is_from_me, media_type, filename, ...)`: `sender_name` is set when the message is stored and updated when a better name arrives. Timestamps are UTC.
- `messages_fts`: FTS5 index on `content`, tokenizer `unicode61 remove_diacritics 2`.

## Tools

| Tool | Notes |
|---|---|
| `whatsapp_status` | Service installed and running, linked, connected, archive size, send folder, and a plain-language `next_step` |
| `connect_whatsapp` | Installs or restarts the service, then returns a pairing code for `phone_number` when the Mac is not linked yet. Reports the account when it already is |
| `disconnect_whatsapp` | Unlinks the Mac from the account, uninstalls the service and, with `delete_archive`, deletes the session, archive, downloaded media and logs (never the send folder). Previews unless `confirm=true` |
| `search_contacts` | Name (accent-insensitive) or digits of a number. Returns the JID to pass elsewhere |
| `list_messages` | Filters `chat_jid`, `sender` (number, JID or name), `query` (full text, word prefixes), `after`, `before`. `include_context` adds surrounding messages |
| `list_chats` | `sort_by` = `last_active` or `name`, optional `query` |
| `get_chat`, `get_direct_chat_by_contact`, `get_contact_chats`, `get_last_interaction`, `get_message_context` | Read-only lookups |
| `send_message`, `send_file`, `send_audio_message` | Return a preview and send nothing unless `confirm=true`. A name that matches several contacts is refused with the list of candidates |
| `download_media` | Downloads once, then returns the cached file |

List tools return `has_more` and `next_cursor`; pass the cursor back as `cursor` for the next page. Read tools are annotated `readOnlyHint`, send tools `destructiveHint` and `openWorldHint`.

## Troubleshooting

| Symptom | Fix |
|---|---|
| `status` says the daemon is not reachable | `whatsapp-mcp install-service`, then read `daemon.log` in the log folder |
| Linux: the service stops when you log out of the Pi | Run `sudo loginctl enable-linger $USER` once |
| Linux: `install-service` says it cannot reach systemd | Log in to the machine (SSH is fine) so that a systemd user session exists, then retry |
| Log says `stream replaced` | Another process uses the same session: stop the original bridge or any hand-started daemon |
| Log says `logged out` | The phone removed this linked device: run `whatsapp-mcp link` (it restarts the service first) or ask Claude to connect WhatsApp again |
| The pairing code is refused or expired | Codes last about two minutes; ask for a new one. Use the number in international format, without a leading 0 |
| `download_media` fails with 403 | WhatsApp no longer stores that file; attachments expire after a few weeks |
| `send_file` says the file is outside the send root | Copy the file into the send folder that `whatsapp_status` reports |
| The extension's reads say WhatsApp is not set up | Ask Claude to connect WhatsApp: the archive is created by the service on first start |

## Development

```bash
go vet ./... && go test ./...
./scripts/whatsmeow-parity.sh   # every whatsmeow method we call still exists
```

`scripts/build-release.sh VERSION` builds the release artifacts into `dist/`: one archive per system and architecture, the Claude Desktop extension with a universal binary, `checksums.txt`, the installer (`install.sh`) and the Homebrew formula (`whatsapp-mcp.rb`, generated by `scripts/homebrew-formula.sh`). The [abonur/homebrew-tap](https://github.com/abonur/homebrew-tap) repository copies the formula from each new release. It needs macOS and Node.js (for `npx @anthropic-ai/mcpb`). The extension's manifest is `extension/manifest.json`.

To publish a release, push a tag: `git tag v0.1.0 && git push origin v0.1.0`. The release workflow (`.github/workflows/release.yml`) runs the tests, builds, signs with a Developer ID certificate, notarizes with Apple and attaches the artifacts to a GitHub release. [RELEASING.md](RELEASING.md) lists the one-time setup: the certificate, the notary key and the repository secrets.

CI (`.github/workflows/whatsapp-go.yml`) checks formatting, vets and runs the tests on Linux x64 (with the race detector), Linux ARM64, Windows and macOS. On each it runs `scripts/smoke.sh`, which starts an unpaired daemon on an empty folder and checks it answers on its socket without trying to pair, and it validates the service definition: `systemd-analyze verify` for the unit, `plutil -lint` for the property list, and a real register, query and delete of the Windows scheduled task. `scripts/test-install.sh` runs the installer against a fake release on localhost (install, upgrade, and refusal of a tampered archive, with curl and with wget), and on macOS `scripts/test-homebrew.sh` installs the formula from a throwaway tap, runs its test and checks the service would use the `opt` path. macOS also packages the release artifacts and the extension. A weekly job tries the latest whatsmeow and reports in a rolling issue when an upgrade is available.

# exo-discord - Access & Delivery

exo-discord uses the existing local Discord access manager before a message ever reaches Claude Code. Unknown DM senders are handled by pairing. Allowlisted users and enabled guild channels pass through. Channel messages still obey `require_mention` and the configured allowlists.

All access state lives under `~/.exo-discord/`. Project config comes from the nearest `.exo-discord` file first, then `~/.exo-discord/config.toml`. Pairing state is persisted separately in `~/.exo-discord/access.json`. The runtime re-evaluates access on every inbound message, so local approvals and removals take effect without restarting the plugin.

The channel injection primitive matches the reverse-engineering artifact at `/Users/alxx/Library/Mobile Documents/iCloud~md~obsidian/Documents/nebula/00-The-Void/Artifacts/2026-04-17-claude-channel-injection-RE.md`: `notifications/claude/channel` over the existing MCP stdio pipe, with the server advertising `experimental["claude/channel"]`. Claude Code wraps the delivered content as a synthetic `<channel ...>` user turn.

## At a glance

| | |
| --- | --- |
| Default DM policy | pairing |
| Config discovery | nearest `.exo-discord`, then `~/.exo-discord/config.toml` |
| Pairing state | `~/.exo-discord/access.json` |
| Channel audit log | `~/.exo-discord/channel-audit.log` |
| Runtime audit log | `~/.exo-discord/audit.log` |

## DMs

Unknown DM senders are not relayed immediately. exo-discord replies with a pairing challenge and drops the message until the operator approves the sender locally.

```bash
exo-discord pair list
exo-discord pair approve <code>
```

Direct allowlist changes stay local:

```bash
exo-discord access allow-user 221773638772129792
exo-discord access remove-user 221773638772129792
```

## Guild Channels

Guild traffic is off unless the channel is explicitly enabled. The effective channel id is the parent channel for threads, so a thread inherits its parent channel policy.

```bash
exo-discord access allow-channel 846209781206941736
exo-discord access remove-channel 846209781206941736
```

With `require_mention = true`, the bot only relays messages that mention or reply to it. Set `require_mention = false` in config to process every message from allowlisted users in enabled channels.

## Delivery

Outbound replies still use the standard MCP tools. Claude calls the existing `reply`, `react`, `edit_message`, `fetch_history`, and `download_attachment` tools over the same MCP stdio session.

Inbound delivery is append-only and audited. `~/.exo-discord/channel-audit.log` records the adapter name, source, timestamp, content hash, and meta keys for every injected channel message. The file rotates at 10 MB to `channel-audit.log.1`, keeping the current log plus one prior rotation. The general runtime audit remains in `~/.exo-discord/audit.log`.

Permission relay is a future feature and is not wired in this plugin yet. Leave `channel.claude.permission_relay = false`, which is now the default, until the request or reply handler exists.

## Development Activation

Install the local bundle:

```bash
claude plugin install file:///path/to/exo-discord/plugin/
```

Run Claude Code with the local plugin channel enabled:

```bash
claude --dangerously-load-development-channels --channels plugin:exo-discord@local
```

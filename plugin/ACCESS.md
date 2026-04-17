# exo-discord - Access & Delivery

exo-discord uses the existing local Discord access manager before a message ever reaches Claude Code. Unknown DM senders are handled by pairing. Allowlisted users and enabled guild channels pass through. Channel messages still obey `require_mention` and the configured allowlists.

All access state lives under `~/.exo-discord/`. Project config comes from the nearest `.exo-discord` file first, then `~/.exo-discord/config.toml`. Pairing state is persisted separately in `~/.exo-discord/access.json`. Long-running runtimes watch both the active config file and the local access state with `github.com/fsnotify/fsnotify`, so local approvals and removals take effect on a running process without restart.

The channel injection primitive matches the reverse-engineering artifact at `/Users/alxx/Library/Mobile Documents/iCloud~md~obsidian/Documents/nebula/00-The-Void/Artifacts/2026-04-17-claude-channel-injection-RE.md`: `notifications/claude/channel` over the existing MCP stdio pipe, with the server advertising `experimental["claude/channel"]`. Claude Code wraps the delivered content as a synthetic `<channel ...>` user turn.

## At a glance

| | |
| --- | --- |
| Default DM policy | pairing |
| Config discovery | nearest `.exo-discord`, then `~/.exo-discord/config.toml` |
| Pairing state | `~/.exo-discord/access.json` |
| Channel audit log | `~/.exo-discord/channel-audit.log` |
| Runtime audit log | `~/.exo-discord/audit.log` |
| Hot reload window | about 100 ms after a local config or state change |

## Hot Reload

`exo-discord bot-mode`, `exo-discord channel-plugin`, and `exo-discord codex-bridge` all hot-reload access policy changes from disk. Config allowlist edits such as `allowed_user_ids` and `allowed_channel_ids`, plus persisted pairing approvals in `~/.exo-discord/access.json`, are picked up automatically by the running runtime after the watcher debounce window.

The runtime audit log records successful reloads as `access_policy_reloaded` entries with a `source` field of `config` or `state`.

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

Those mutations take effect on running runtimes in about 100 ms.

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

## Codex Thread Namespace

Codex maintains threads inside the app-server process a client is connected to. The app-server that `codex app-server --listen ws://...` spawns has its own in-memory thread table: a thread id that was created by `codex` (interactive shell, source kind `cli`) is not the same thing as a thread loaded in the app-server. The rollout files on disk at `$CODEX_HOME/rollouts/` are shared, so an interactive thread id can usually be rehydrated via `thread/resume`, but `turn/start` requires the thread to be loaded in memory first.

`exo-discord codex-bridge` handles this automatically:

1. Try `turn/start` with the requested thread id (from `--thread` or `~/.exo-discord/codex-thread.json`).
2. On `thread not found`: call `thread/resume` with the same id so the app-server pulls it from disk. This is the typical path when the id was taken from an interactive codex shell.
3. If resume fails: call `thread/list` to discover another live thread.
4. If discovery is empty AND `--auto-create-thread` is on (default): call `thread/start` to open a brand new thread on the app-server, cache it, and retry `turn/start`.
5. If `--no-auto-create-thread` (or `channel.codex.auto_create_thread = false`) is set and discovery is empty: fail with a clear error listing the recovery options.

Delivery outcomes are audited in two places with different coverage:

- Runtime slog stream emits a full `codex delivery outcome` record on every attempt (success or failure) with `original_thread_id`, `final_thread_id`, `fallback_path` (`none | resume | discover | auto_create | cached | error`), and `result` (`success | error`).
- `channel-audit.log` captures only successful injections and carries `original_thread_id`, `fallback_path`, plus the existing privacy-preserving meta keys (content hash, not content). Failures are not written here by design - check the slog stream or `~/.exo-discord/audit.log` for failure details.

## Mirroring codex replies back to discord

`exo-discord codex-bridge --mirror-responses` tells the bridge to post the codex reply text back into the originating discord channel as a threaded reply. With the flag off, the bridge is one-way: discord DM in, codex turn started, no outbound reply.

The app-server speaks the `app-server-protocol` v2 notification stream. After `turn/start`, codex streams notifications unsolicited (no subscribe RPC is required). The reply arrives as a sequence of `item/completed` notifications carrying `ThreadItem::AgentMessage { text, phase }`, followed by a `turn/completed` notification whose `turn.items` slice is empty by contract. The bridge buffers agent messages per turn id, prefers `phase=final_answer` when present, otherwise falls back to the last buffered agent message. Legacy `turn/completed` payloads that still carry unphased items keep the older joined-message fallback for compatibility.

Each successful mirror emits a `codex mirror emitted` slog record with `event=mirror_emitted`, `turn_id`, `thread_id`, `channel_id`, `message_id` (the inbound discord message the reply threads under), `chunk_count`, and `char_count`. Failures emit `codex mirror reply failed` with the same keys plus the error.

Replies longer than 2000 characters are split across discord messages: the first chunk is posted as a reply to the originating message, follow-up chunks are sent as plain channel messages. Shutdown (`Close`) cancels the mirror service context so a pending Reply call unblocks cleanly, waits for any in-flight mirror goroutine before returning, and drops buffered turn state on disconnect because the app-server lost that turn state too.

## Development Activation

Install the local bundle:

```bash
claude plugin install file:///path/to/exo-discord/plugin/
```

Run Claude Code with the local plugin channel enabled:

```bash
claude --dangerously-load-development-channels --channels plugin:exo-discord@local
```

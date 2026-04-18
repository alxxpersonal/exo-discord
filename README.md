# Exo Discord

**Plug AI agents into Discord. Single Go binary, MCP server, official bot platform, no hosted relay.**

## Overview

Exo Discord is a Go CLI that turns any directory into a live Discord agent workspace. Run a bot, wire it into a Claude Code session as a native channel plugin, bridge inbound messages into a running Codex `app-server`, or all three at once. One binary, one config file, full audit log.

> Built for developers who want their own AI agent reachable on Discord without a hosted relay, an unofficial client, or a custom protocol fork.

## Quickstart

Requires [Go 1.23+](https://go.dev/dl/).

```bash
# Install from a tagged release
go install github.com/alxxpersonal/exo-discord/cmd/exo-discord@latest

# Or build from source
git clone https://github.com/alxxpersonal/exo-discord.git
cd exo-discord
make install-bin
```

```bash
exo-discord init                              # generate .exo-discord starter config
printf '%s\n' "$DISCORD_BOT_TOKEN" \
  | exo-discord configure --bot-token-stdin   # save bot token to local config (0600 perms)
exo-discord access --add-user "$USER_ID"      # allowlist a discord user id
exo-discord doctor --json                     # verify config discovery and perms
exo-discord bot-mode                          # start the gateway, accept allowlisted DMs
exo-discord listen --dry-run                  # tail inbound envelopes
exo-discord send --channel "$CID" --text "hi" # send a message
```

## Why

Most Discord agent setups fall into one of three buckets: unofficial account automation, hosted relay services, or one-off bot scripts that mix platform glue with agent logic. Exo Discord aims for the cleaner middle ground - official Discord integration, local policy enforcement, and a stable surface for agents to act through.

## Modes

| | |
|---|---|
| **Bot mode** | Standard Discord bot gateway with allowlists, pairing approvals, and audit log |
| **Channel plugin** | Claude Code plugin: discord messages inject as synthetic user turns via `notifications/claude/channel` MCP primitive |
| **Codex bridge** | Connects to `codex app-server` and pushes `turn/start` per discord message, mirrors `item/completed` text back |
| **User install** | OAuth 2.0 user-authorization flow, multi-user token storage, no self-bot |
| **Manage tree** | Full guild management surface - channel, role, member, message, embed, interactions, REST escape hatch, declarative YAML scaffold, yaegi-sandboxed exec |

## Features

| | |
|---|---|
| **Single binary** | One Go CLI owns runtime flow, config discovery, MCP, hooks, audit |
| **Local-first** | Config + tokens + audit log live under `.exo-discord` or `~/.exo-discord/` with strict 0600/0700 perms |
| **Hot-reload** | Allowlist mutations via `access --add-user` apply within 100ms via fsnotify, no restart needed |
| **Atomic state** | Token cache + access state use temp-then-rename writes, survive process death |
| **MCP control** | Agents call `send_message`, `reply`, `react`, `edit_message`, `fetch_history`, `download_attachment`, `set_status` plus the full manage surface |
| **Channel injection** | First-class Claude Code integration via the same MCP notification primitive Anthropic's official discord plugin uses |
| **Codex bridge** | Auto-discovers active codex thread, falls back to `thread/resume` then `thread/start`, retries `turn/start` cleanly |
| **Mirror responses** | Codex agent replies post back to the originating Discord channel with chunking and shutdown drain |
| **Sandboxed exec** | `manage exec` runs Go scripts via [yaegi](https://github.com/traefik/yaegi) with a strict 7-package whitelist, AST import validation, ctx timeout, and panic recover |
| **Declarative scaffold** | YAML guild specs diffed against live state, atomic apply with rollback, opt-in delete-extras |
| **Auditable** | Inbound + outbound + access decisions logged at `~/.exo-discord/audit.log` and `~/.exo-discord/channel-audit.log` with sha256 hashing and 10MB rotation |

## Plug into Claude Code

Load the plugin bundle inline from the repo and start a session with the channel active:

```bash
claude --plugin-dir ~/Code/Self/exo-discord/plugin \
       --dangerously-load-development-channels plugin:exo-discord@inline
```

Inbound DMs from allowlisted users land in the running Claude Code session as synthetic user turns wrapped in `<channel source="discord" chat_id="..." user="..." ts="...">message</channel>`. Claude replies via the standard MCP tool surface (`reply`, `react`, etc) and the plugin posts back to Discord through `discordgo`.

## Plug into Codex

Run a long-lived `codex app-server` and bridge into it:

```bash
codex app-server --listen ws://127.0.0.1:8765       # in one terminal
exo-discord codex-bridge \
  --transport ws \
  --websocket-url ws://127.0.0.1:8765 \
  --mirror-responses                                  # in another
```

Bridge auto-discovers the active thread (or use `--thread <id>` to pin one). On `thread not found` it retries via `thread/resume`, then `thread/list` discovery, then auto-creates a new thread (disable with `--no-auto-create-thread`). Codex `item/completed` agent messages mirror back to the originating Discord DM; `turn/completed` flushes the buffered reply.

## Auth

Two authentication paths, both local-only:

- **Bot token** - standard Discord bot platform. Token saved at `~/.exo-discord/token` (0600), passed via stdin only, never on the command line. Required for `bot-mode`, `channel-plugin`, `codex-bridge`, and most `manage` operations.
- **User install OAuth 2.0** - official user-authorization flow. Multi-user token storage at `~/.exo-discord/oauth/<user_id>.json` (0600). CSRF state validated with `crypto/rand` + 10 min TTL. No self-bot, no reverse-engineered client.

All token files use 0600, all dirs 0700. Tokens never echoed to stdout. Logs redact token fields.

## All Commands

```
init              Create a starter .exo-discord config file
configure         Update config keys via stdin (bot tokens never on command line)
doctor            Validate config discovery, paths, perms, allowlist state
access            Inspect or mutate the local allowlist (--add-user, --add-channel)
pair              Inspect or resolve pending channel pairings
auth              user-install oauth: login, list, revoke (--as-user, --force)
listen            Print inbound discord envelopes (--dry-run for hook simulation)
send              Send a message, reply, react, edit (--channel, --reply, --emoji)
bot-mode          Run the discord gateway loop (standard bot)
channel-plugin    Run the claude-code channel plugin bridge over stdio
codex-bridge      Bridge inbound discord messages into a running codex app-server
mcp               MCP server subcommands (mcp serve)
manage            Full guild management tree:
                    guild   - list-channels / list-roles / list-members / inspect
                    channel - list / create / update / delete / perm-set / perm-remove
                    role    - list / create / update / delete / assign / unassign
                    member  - list / get / kick / ban
                    message - send / edit / delete / bulk-delete / react
                    embed   - post / add-button / add-select
                    interactions - listen / respond
                    rest    - <METHOD> <PATH> [--body] escape hatch
                    scaffold - declarative YAML diff + apply
                    exec    - yaegi-sandboxed go script runner
```

All destructive operations (delete, ban, kick, bulk-delete, scaffold apply, exec) require `--yes` on the CLI and `confirm: true` in the MCP tool args.

## Architecture

```
discord gateway
  -> exo-discord (allowlist + audit)
    -> hook dispatch (HTTP POST or stdio JSON)
    -> claude-code via notifications/claude/channel (channel-plugin)
    -> codex app-server via turn/start JSON-RPC (codex-bridge)
    -> MCP tool surface (any agent that registers the server)
```

Config discovery walks up from CWD looking for `.exo-discord`, falls back to `~/.exo-discord/config.toml`. Same TOML schema both ways. Multiple agents can share one bot token via the allowlist + pairing flow.

## Contributing

PRs welcome. Run `make lint && make test` before submitting. By opening a PR you agree to license your contribution under the [MIT License](LICENSE).

## Legal

Operates through Discord's official application model. You are responsible for your bot token, server policy, and compliance with Discord's developer terms. The user-install mode uses official OAuth 2.0; no self-bot or reverse-engineered transports.

## License

[MIT](LICENSE)

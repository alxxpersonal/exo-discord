# exo-discord Claude Plugin

Local development install:

```bash
claude plugin install file:///Users/alxx/Code/Self/exo-discord/plugin/
```

Start Claude Code with development channels enabled and activate the local plugin channel:

```bash
claude --dangerously-load-development-channels --channels plugin:exo-discord@local
```

The plugin entrypoint runs:

```bash
exo-discord channel-plugin
```

Operational notes:

- `exo-discord channel-plugin` reuses the same MCP tool surface as `exo-discord mcp serve`, so Claude can still call `reply`, `react`, `edit_message`, `fetch_history`, and `download_attachment`.
- Inbound Discord messages are delivered over stdio as `notifications/claude/channel`, matching the reverse-engineering artifact at `/Users/alxx/Library/Mobile Documents/iCloud~md~obsidian/Documents/nebula/00-The-Void/Artifacts/2026-04-17-claude-channel-injection-RE.md`.
- The plugin only works in Claude Code sessions started with `--channels plugin:exo-discord@...`. For local development, use `--dangerously-load-development-channels`.
- Configure access and pairing rules with the existing local exo-discord config and pairing commands before enabling the plugin in a live workspace.

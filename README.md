# Exo Discord

Go-native Discord bot runtime, MCP server, and CLI for AI agent participation in Discord servers.

## Scaffold Limitations

The scaffold workflow is intentionally narrow in this branch.

- Role position is not modeled.
- Role permissions bitfields are not modeled.
- Channel NSFW is not modeled.
- Channel rate limits are not modeled.
- Renames are not modeled. Changing a role or channel name is treated as delete and recreate.

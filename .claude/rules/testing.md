---
paths: ["**/*_test.go", "**/testdata/**"]
---

# Testing Conventions

- Default to adjacent package tests with Go's `testing` package. Use `require` for fatal setup checks and `assert` for follow-up expectations when `testify` improves clarity.
- Table-driven tests are required for config search paths, permission mode validation, pairing state transitions, allowlist results, hook decision parsing, and MCP tool input validation.
- Normal test runs stay offline. Do not hit the real Discord API, live hook endpoints, or other external services outside env-gated E2E coverage.
- Use temp dirs, fakes, `httptest`, and pipe-based stdio harnesses for config, runtime, hook, CLI, and MCP tests.
- E2E coverage runs only when `EXO_DISCORD_E2E=1` plus the required Discord test env vars are set.
- Golden tests are for inbound hook envelopes, MCP tool schemas, and `doctor --json` output.
- No flaky timing-based tests without fake clock support.
- Every test must assert concrete state or output. No assertion-free tests.

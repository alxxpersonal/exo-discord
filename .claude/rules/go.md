---
paths: ["cmd/**/*.go", "internal/**/*.go"]
---

# Go Conventions

- Standard goimports grouping: stdlib, external, local (blank line separators)
- Section separators: `// --- Section Name ---` with proper capitalization
- Exported functions MUST have doc comments starting with function name (`// NewClient creates a new API client.`)
- Doc comments use third-person present tense verbs: creates, returns, handles, updates
- Inline comments: lowercase first letter, above the line, not end of line
- Error messages lowercase: `fmt.Errorf("loading config: %w", err)`
- Wrap errors with `%w`, never `%v` for wrapped errors
- Error messages prefix with operation context: `"read config: %w"`, not just `"error: %w"`
- Wrap at call site once, propagate bare errors up from API/DB layers
- Use `errors.Is()` for sentinel checks (`os.ErrNotExist`, `syscall.EPERM`, etc)
- Custom error types implement `Error() string` with nil-safe receivers
- Validation errors include concrete context (line numbers, expected/actual values)
- NEVER panic in library code. Return errors.
- NO em/en dashes (U+2014, U+2013) anywhere in code, comments, strings, or docs
- Token/secret files: 0600 perms, 0700 on dirs under `~/.exo-discord/`
- All API types use `json` struct tags

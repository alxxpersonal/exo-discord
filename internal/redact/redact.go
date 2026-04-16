package redact

import (
	"regexp"
	"strings"
)

// --- Constants ---

const placeholder = "[redacted]"

// --- Patterns ---

var (
	authSchemePattern = regexp.MustCompile(`(?i)\b(bot|bearer)\s+([^\s,;]+)`)
	tokenKeyPattern   = regexp.MustCompile(`(?i)\b([a-z0-9_]*token[a-z0-9_]*|authorization)\b(\s*[:=]\s*)((?:(?:bot|bearer)\s+[^\s,;]+)|"[^"\n]*"|'[^'\n]*'|[^\s,;]+)`)
	discordTokenRegex = regexp.MustCompile(`\b[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{20,}\b`)
	genericTokenRegex = regexp.MustCompile(`^[A-Za-z0-9._-]{24,}$`)
)

// --- Public Helpers ---

// String redacts secret-like values in a string.
func String(value string) string {
	if value == "" {
		return ""
	}

	redacted := tokenKeyPattern.ReplaceAllStringFunc(value, redactTokenValue)
	redacted = authSchemePattern.ReplaceAllStringFunc(redacted, redactAuthScheme)
	redacted = discordTokenRegex.ReplaceAllString(redacted, placeholder)
	return redacted
}

// WrapError wraps an error so its string form stays redacted.
func WrapError(err error) error {
	if err == nil {
		return nil
	}
	return wrappedError{err: err}
}

// --- Error Wrappers ---

type wrappedError struct {
	err error
}

// Error returns the redacted error string.
func (w wrappedError) Error() string {
	return String(w.err.Error())
}

// Unwrap returns the original wrapped error.
func (w wrappedError) Unwrap() error {
	return w.err
}

// --- Redaction Helpers ---

func redactTokenValue(match string) string {
	parts := tokenKeyPattern.FindStringSubmatch(match)
	if len(parts) != 4 {
		return match
	}
	if !isSensitiveKey(parts[1]) {
		return match
	}
	return parts[1] + parts[2] + maskValue(parts[3])
}

func redactAuthScheme(match string) string {
	parts := authSchemePattern.FindStringSubmatch(match)
	if len(parts) != 3 {
		return match
	}
	if !looksLikeToken(parts[2]) {
		return match
	}
	return parts[1] + " " + placeholder
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	return normalized == "authorization" ||
		normalized == "token" ||
		strings.HasSuffix(normalized, "_token")
}

func looksLikeToken(value string) bool {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"'`)
	if trimmed == "" {
		return false
	}
	return discordTokenRegex.MatchString(trimmed) || genericTokenRegex.MatchString(trimmed)
}

func maskValue(value string) string {
	switch {
	case len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"':
		return `"` + placeholder + `"`
	case len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'':
		return `'` + placeholder + `'`
	default:
		return placeholder
	}
}

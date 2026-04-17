package channelbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Test Cases ---

func TestAuditWriterAppendCreatesLogWithStrictPerms(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	writer := NewAuditWriter(homeDir)
	writer.now = func() time.Time {
		return time.Unix(1_713_000_000, 0).UTC()
	}

	err := writer.Append("claude", "discord", "hello world", map[string]any{
		"user": "alice",
		"ts":   "2026-04-17T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}

	data, err := os.ReadFile(writer.Path())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want 1", len(lines))
	}

	var record struct {
		Timestamp     time.Time `json:"ts"`
		Adapter       string    `json:"adapter"`
		Source        string    `json:"source"`
		ContentSHA256 string    `json:"content_sha256"`
		MetaKeys      []string  `json:"meta_keys"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}

	if record.Adapter != "claude" {
		t.Fatalf("adapter = %q, want claude", record.Adapter)
	}
	if record.Source != "discord" {
		t.Fatalf("source = %q, want discord", record.Source)
	}
	if record.ContentSHA256 != "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9" {
		t.Fatalf("content hash = %q", record.ContentSHA256)
	}
	if got := strings.Join(record.MetaKeys, ","); got != "ts,user" {
		t.Fatalf("meta keys = %q, want ts,user", got)
	}

	info, err := os.Stat(writer.Path())
	if err != nil {
		t.Fatalf("Stat(file) error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file perms = %04o, want 0600", info.Mode().Perm())
	}

	dirInfo, err := os.Stat(filepath.Dir(writer.Path()))
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("dir perms = %04o, want 0700", dirInfo.Mode().Perm())
	}
}

func TestAuditWriterAppendRejectsMissingFields(t *testing.T) {
	t.Parallel()

	writer := NewAuditWriter(t.TempDir())

	if err := writer.Append("", "discord", "hello", nil); err == nil {
		t.Fatal("Append() error = nil, want adapter error")
	}
	if err := writer.Append("claude", "", "hello", nil); err == nil {
		t.Fatal("Append() error = nil, want source error")
	}
}

func TestAuditWriterAppendReusesExistingFile(t *testing.T) {
	t.Parallel()

	writer := NewAuditWriter(t.TempDir())
	if err := writer.Append("claude", "discord", "one", nil); err != nil {
		t.Fatalf("Append(first) error = %v", err)
	}
	if err := writer.Append("claude", "discord", "two", nil); err != nil {
		t.Fatalf("Append(second) error = %v", err)
	}

	data, err := os.ReadFile(writer.Path())
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := len(strings.Split(strings.TrimSpace(string(data)), "\n")); got != 2 {
		t.Fatalf("line count = %d, want 2", got)
	}
}

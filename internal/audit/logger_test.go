package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// --- Constants ---

const sampleToken = "MTg0Njk1MDgwNzA5MzI0ODAw.ABCdef.gHIjklMNOpqrstUVWXYZ123456"

// --- Test Cases ---

func TestLoggerWriteCreatesPrivateJSONL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "audit.log")
	logger := NewLogger(path)
	logger.now = func() time.Time { return time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC) }

	if err := logger.Write(Record{
		Mode:      "bot",
		Event:     "hook_decision",
		MessageID: "1352053123456789012",
		ChannelID: "846209781206941736",
		UserID:    "184695080709324800",
		Decision:  "reply",
		Error:     "discord login failed with token " + sampleToken,
		Fields: map[string]string{
			"authorization": "Bot " + sampleToken,
		},
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if err := logger.Write(Record{
		Timestamp: time.Date(2026, 4, 17, 0, 0, 1, 0, time.FixedZone("UTC+2", 2*60*60)),
		Event:     "message_received",
	}); err != nil {
		t.Fatalf("Write() second error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("file perms = %04o, want %04o", got, want)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", filepath.Dir(path), err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("dir perms = %04o, want %04o", got, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if got, want := len(lines), 2; got != want {
		t.Fatalf("line count = %d, want %d", got, want)
	}

	var first Record
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("Unmarshal(first) error = %v", err)
	}
	if got, want := first.Timestamp, time.Date(2026, 4, 16, 23, 59, 59, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("first timestamp = %v, want %v", got, want)
	}
	if strings.Contains(first.Error, sampleToken) {
		t.Fatalf("first error = %q, want redacted token", first.Error)
	}
	if got, want := first.Fields["authorization"], "Bot [redacted]"; got != want {
		t.Fatalf("authorization field = %q, want %q", got, want)
	}

	var second Record
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("Unmarshal(second) error = %v", err)
	}
	if got, want := second.Timestamp, time.Date(2026, 4, 16, 22, 0, 1, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("second timestamp = %v, want %v", got, want)
	}
}

func TestLoggerWriteRejectsExistingAuditFilePerms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "state")
	path := filepath.Join(dir, "audit.log")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	//nolint:gosec
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	logger := NewLogger(path)
	err := logger.Write(Record{Event: "hook_decision"})
	if err == nil {
		t.Fatal("Write() error = nil, want perms error")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Fatalf("Write() error = %v, want 0600 error", err)
	}
}

func TestLoggerWriteRejectsExistingAuditDirPerms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dir := filepath.Join(root, "state")
	//nolint:gosec
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	logger := NewLogger(filepath.Join(dir, "audit.log"))
	err := logger.Write(Record{Event: "hook_decision"})
	if err == nil {
		t.Fatal("Write() error = nil, want dir perms error")
	}
	if !strings.Contains(err.Error(), "0700") {
		t.Fatalf("Write() error = %v, want 0700 error", err)
	}
}

func TestLoggerWriteRejectsEmptyEvent(t *testing.T) {
	t.Parallel()

	logger := NewLogger(filepath.Join(t.TempDir(), "audit.log"))
	if err := logger.Write(Record{}); err == nil {
		t.Fatal("Write() error = nil, want error")
	}
}

package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Cases ---

func TestChannelPluginCommandInitializeAndNotificationFlow(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\nallowed_user_ids = [\"user-1\"]\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderr := &bytes.Buffer{}

	session := &fakeSession{}
	manager := &fakeManager{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    stdinReader,
		Stdout:   stdoutWriter,
		Stderr:   stderr,
		Context: func() context.Context {
			return ctx
		},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		NewManager: func(config.ResolvedConfig) (discordpkg.Manager, error) {
			return manager, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"channel-plugin"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	reader := bufio.NewReader(stdoutReader)
	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "0.1.0",
			},
		},
	})

	initializeResponse := readJSONLine(t, reader)
	capabilities := initializeResponse["result"].(map[string]any)["capabilities"].(map[string]any)
	experimental, ok := capabilities["experimental"].(map[string]any)
	if !ok {
		t.Fatalf("initialize capabilities = %#v, want experimental map", capabilities)
	}
	if _, ok := experimental["claude/channel"]; !ok {
		t.Fatalf("experimental capabilities = %#v, missing claude/channel", experimental)
	}

	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	waitFor(t, time.Second, func() bool {
		return session.OpenCount() == 1
	})

	go session.emit(context.Background(), discordpkg.Message{
		ID:             "msg-1",
		ChannelID:      "chan-1",
		ChannelKind:    discordpkg.ChannelKindDM,
		AuthorID:       "user-1",
		AuthorUsername: "alice",
		Content:        "hello from discord",
		Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
	})

	notification := readJSONLine(t, reader)
	if notification["method"] != "notifications/claude/channel" {
		t.Fatalf("notification method = %#v, want notifications/claude/channel", notification["method"])
	}

	cancel()
	_ = stdinWriter.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel-plugin command did not shut down")
	}

	if session.CloseCount() != 1 {
		t.Fatalf("session close count = %d, want 1", session.CloseCount())
	}
	if manager.OpenCount() != 1 || manager.CloseCount() != 1 {
		t.Fatalf("manager open=%d close=%d, want 1/1", manager.OpenCount(), manager.CloseCount())
	}
}

func TestChannelPluginCommandWritesChannelAuditLog(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\nallowed_user_ids = [\"user-1\"]\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()
	stderr := &bytes.Buffer{}

	session := &fakeSession{}
	manager := &fakeManager{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    stdinReader,
		Stdout:   stdoutWriter,
		Stderr:   stderr,
		Context: func() context.Context {
			return ctx
		},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		NewManager: func(config.ResolvedConfig) (discordpkg.Manager, error) {
			return manager, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"channel-plugin"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	reader := bufio.NewReader(stdoutReader)
	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": "2025-03-26",
			"capabilities":    map[string]any{},
			"clientInfo": map[string]any{
				"name":    "test-client",
				"version": "0.1.0",
			},
		},
	})
	_ = readJSONLine(t, reader)

	writeJSONLine(t, stdinWriter, map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})

	waitFor(t, time.Second, func() bool {
		return session.OpenCount() == 1
	})

	content := "hello from discord"
	go session.emit(context.Background(), discordpkg.Message{
		ID:             "msg-1",
		ChannelID:      "chan-1",
		ChannelKind:    discordpkg.ChannelKindDM,
		AuthorID:       "user-1",
		AuthorUsername: "alice",
		Content:        content,
		Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
	})

	notification := readJSONLine(t, reader)
	if notification["method"] != "notifications/claude/channel" {
		t.Fatalf("notification method = %#v, want notifications/claude/channel", notification["method"])
	}

	auditPath := filepath.Join(homeDir, ".exo-discord", "channel-audit.log")
	expectedHash := sha256HexForTest(content)
	waitFor(t, time.Second, func() bool {
		data, err := os.ReadFile(auditPath)
		if err != nil {
			return false
		}
		return strings.Contains(string(data), "\"content_sha256\":\""+expectedHash+"\"")
	})

	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", auditPath, err)
	}
	if !strings.Contains(string(data), "\"content_sha256\":\""+expectedHash+"\"") {
		t.Fatalf("audit log = %q, want hash %s", string(data), expectedHash)
	}

	cancel()
	_ = stdinWriter.Close()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel-plugin command did not shut down")
	}
}

// --- Helpers ---

func writeJSONLine(t *testing.T, writer *io.PipeWriter, value any) {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if _, err := writer.Write(append(data, '\n')); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
}

func readJSONLine(t *testing.T, reader *bufio.Reader) map[string]any {
	t.Helper()

	line, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("ReadBytes() error = %v", err)
	}

	var value map[string]any
	if err := json.Unmarshal(line, &value); err != nil {
		t.Fatalf("Unmarshal() error = %v, line=%q", err, line)
	}
	return value
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not met before timeout")
}

func sha256HexForTest(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

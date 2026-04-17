package channelbridge

import (
	"bytes"
	"path/filepath"
	"testing"
)

// --- Test Cases ---

func TestNewAdapterRequiresEnabledAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewAdapter(Config{}, HookEnv{})
	if err == nil {
		t.Fatal("NewAdapter() error = nil, want error")
	}
}

func TestNewAdapterRejectsUnsupportedAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewAdapter(Config{
		Enabled: []string{"unknown"},
	}, HookEnv{})
	if err == nil {
		t.Fatal("NewAdapter() error = nil, want error")
	}
}

func TestNewAdapterReturnsClaudeAdapter(t *testing.T) {
	t.Parallel()

	adapter, err := NewAdapter(Config{
		Enabled: []string{"claude"},
	}, HookEnv{
		HomeDir:      t.TempDir(),
		ClaudeWriter: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter.Name() != "claude" {
		t.Fatalf("adapter name = %q, want claude", adapter.Name())
	}
}

func TestNewAdapterReturnsMultiAdapter(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(t.TempDir(), "broker.sock")

	adapter, err := NewAdapter(Config{
		Enabled: []string{"claude", "codex"},
		Codex: CodexConfig{
			Transport:  codexTransportUnix,
			SocketPath: socketPath,
		},
	}, HookEnv{
		HomeDir:      t.TempDir(),
		ClaudeWriter: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatalf("NewAdapter() error = %v", err)
	}

	if adapter.Name() != "multi" {
		t.Fatalf("adapter name = %q, want multi", adapter.Name())
	}
}

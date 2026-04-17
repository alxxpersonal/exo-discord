package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/channelbridge"
	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeChannelAdapter struct{}

func (fakeChannelAdapter) Name() string { return "codex" }

func (fakeChannelAdapter) Deliver(context.Context, channelbridge.Event) error { return nil }

func (fakeChannelAdapter) Close() error { return nil }

type captureChannelAdapter struct {
	mu     sync.Mutex
	events []channelbridge.Event
}

func (a *captureChannelAdapter) Name() string { return "codex" }

func (a *captureChannelAdapter) Deliver(_ context.Context, event channelbridge.Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, event)
	return nil
}

func (a *captureChannelAdapter) Close() error { return nil }

func (a *captureChannelAdapter) Count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.events)
}

// --- Test Cases ---

func TestCodexBridgeCommandAppearsOnRoot(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(Environment{
		StartDir: t.TempDir(),
		HomeDir:  t.TempDir(),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})

	names := make([]string, 0, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		names = append(names, child.Name())
	}

	if !slices.Contains(names, "codex-bridge") {
		t.Fatalf("root commands = %v, missing codex-bridge", names)
	}
}

func TestCodexBridgeCommandValidatesTransport(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"codex-bridge", "--transport", "ws"})
	err := cmd.Execute()
	if err == nil || err.Error() != "codex websocket transport requires --websocket-url" {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestCodexBridgeCommandReturnsAdapterCreationError(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	wantErr := errors.New("no codex threads were returned by thread/list")
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return &fakeSession{}, nil
		},
		NewChannelAdapter: func(channelbridge.Config, channelbridge.HookEnv) (channelAdapter, error) {
			return nil, wantErr
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"codex-bridge", "--transport", "unix", "--socket", "/tmp/broker.sock"})
	err := cmd.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
}

func TestCodexBridgeCommandUsesInjectedAdapter(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	session := &fakeSession{}
	var gotCfg channelbridge.Config
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		Context: func() context.Context {
			return ctx
		},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		NewChannelAdapter: func(cfg channelbridge.Config, hookEnv channelbridge.HookEnv) (channelAdapter, error) {
			gotCfg = cfg
			if hookEnv.Session != session {
				t.Fatalf("hook env session = %#v, want fake session", hookEnv.Session)
			}
			return fakeChannelAdapter{}, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"codex-bridge", "--transport", "unix", "--socket", "/tmp/broker.sock", "--thread", "thread-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if gotCfg.Codex.Transport != "unix" || gotCfg.Codex.SocketPath != "/tmp/broker.sock" || gotCfg.Codex.ThreadID != "thread-1" {
		t.Fatalf("codex config = %#v", gotCfg.Codex)
	}
	if session.OpenCount() != 1 || session.CloseCount() != 1 {
		t.Fatalf("session open=%d close=%d, want 1/1", session.OpenCount(), session.CloseCount())
	}
}

func TestCodexBridgeCommandHotReloadsAccessAllowlist(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	ctx, cancel := context.WithCancel(context.Background())
	session := &fakeSession{}
	adapter := &captureChannelAdapter{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		Context: func() context.Context {
			return ctx
		},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		NewChannelAdapter: func(channelbridge.Config, channelbridge.HookEnv) (channelAdapter, error) {
			return adapter, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"codex-bridge", "--transport", "unix", "--socket", "/tmp/broker.sock"})

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	waitForCondition(t, func() bool { return session.OpenCount() == 1 })
	runAccessAllowUser(t, startDir, homeDir, "user-1")

	auditPath := filepath.Join(homeDir, ".exo-discord", "audit.log")
	waitForCondition(t, func() bool {
		data, err := os.ReadFile(auditPath)
		if err != nil {
			return false
		}
		return strings.Contains(string(data), "\"event\":\"access_policy_reloaded\"") &&
			strings.Contains(string(data), "\"source\":\"config\"")
	})

	session.emit(context.Background(), discordpkg.Message{
		ID:          "msg-1",
		ChannelID:   "dm-1",
		ChannelKind: discordpkg.ChannelKindDM,
		AuthorID:    "user-1",
	})

	waitForCondition(t, func() bool {
		return adapter.Count() == 1
	})

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("codex-bridge command did not shut down")
	}

	if got := session.SendRequest().Text; got != "" {
		t.Fatalf("pairing message = %q, want no pairing prompt after reload", got)
	}
}

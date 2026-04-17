package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Cases ---

func TestDefaultSessionFactory(t *testing.T) {
	t.Parallel()

	if _, err := defaultSessionFactory(config.ResolvedConfig{
		Config: config.Config{Mode: config.ModeUserInstall},
	}); err == nil || err.Error() != "user_install mode is not implemented" {
		t.Fatalf("defaultSessionFactory(user_install) error = %v", err)
	}

	session, err := defaultSessionFactory(config.ResolvedConfig{
		Config: config.Config{Mode: config.ModeBot, BotToken: "secret"},
	})
	if err != nil {
		t.Fatalf("defaultSessionFactory(bot) error = %v", err)
	}
	if session == nil {
		t.Fatal("defaultSessionFactory(bot) returned nil session")
	}
}

func TestDefaultListenRunner(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	request := listenRequest{
		Config: config.ResolvedConfig{
			Config: config.Config{
				Mode:              config.ModeBot,
				AllowedChannelIDs: []string{"chan-1"},
				Logging:           config.LoggingConfig{Level: config.LogLevelDebug},
			},
			AccessStatePath: filepath.Join(t.TempDir(), "access.json"),
			AuditLogPath:    filepath.Join(t.TempDir(), "audit.log"),
		},
		Session: session,
		Stdout:  stdout,
		Stderr:  stderr,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- defaultListenRunner{}.Run(ctx, request)
	}()

	waitForCondition(t, func() bool { return session.OpenCount() == 1 })
	session.emit(context.Background(), discordpkg.Message{
		ID:          "msg-1",
		ChannelID:   "chan-1",
		ChannelKind: discordpkg.ChannelKindGuildText,
		AuthorID:    "user-1",
	})
	cancel()

	err := <-done
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), `"source":"discord"`) {
		t.Fatalf("stdout = %q, want envelope json", stdout.String())
	}
	if got := session.CloseCount(); got != 1 {
		t.Fatalf("close count = %d, want 1", got)
	}
}

func TestDefaultBotModeRunner(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	request := botModeRequest{
		Config: config.ResolvedConfig{
			Config: config.Config{
				Mode:              config.ModeBot,
				AllowedChannelIDs: []string{"chan-1"},
				RequireMention:    true,
				Hook: config.HookConfig{
					Kind:    config.HookKindStdio,
					Timeout: config.Duration(time.Second),
					Stdio: config.HookStdioConfig{
						Command: []string{"sh", "-c", "cat >/dev/null; printf '{\"decision\":\"reply\",\"reply\":{\"text\":\"ready\"}}'"},
					},
				},
				Status: config.StatusConfig{
					Presence: config.PresenceOnline,
				},
			},
			AccessStatePath: filepath.Join(t.TempDir(), "access.json"),
			AuditLogPath:    filepath.Join(t.TempDir(), "audit.log"),
		},
		Session: session,
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- defaultBotModeRunner{}.Run(ctx, request)
	}()

	waitForCondition(t, func() bool { return session.OpenCount() == 1 })
	session.emit(context.Background(), discordpkg.Message{
		ID:           "msg-1",
		ChannelID:    "chan-1",
		GuildID:      "guild-1",
		ChannelKind:  discordpkg.ChannelKindGuildText,
		AuthorID:     "user-1",
		MentionedBot: true,
	})
	waitForCondition(t, func() bool { return session.ReplyRequest().Text == "ready" })
	cancel()

	err := <-done
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if got := session.StatusRequest().Presence; got != "online" {
		t.Fatalf("Presence = %q, want online", got)
	}
	if got := session.ReplyRequest().Text; got != "ready" {
		t.Fatalf("reply text = %q, want ready", got)
	}
}

func TestNewHookClientAndHelpers(t *testing.T) {
	t.Parallel()

	if _, err := newHookClient(config.HookConfig{Kind: config.HookKindHTTP}, io.Discard); err == nil {
		t.Fatal("newHookClient(http) error = nil, want missing url error")
	}
	if client, err := newHookClient(config.HookConfig{Kind: config.HookKindNone}, io.Discard); err != nil || client != nil {
		t.Fatalf("newHookClient(none) = %#v, %v; want nil, nil", client, err)
	}

	logger := newLogger(config.LoggingConfig{Level: config.LogLevelDebug, Format: config.LogFormatJSON}, io.Discard)
	if logger == nil {
		t.Fatal("newLogger() returned nil")
	}

	status := statusRequestFromConfig(config.StatusConfig{
		Presence:     config.PresenceOnline,
		ActivityType: config.ActivityTypeWatching,
		ActivityText: "tests",
	})
	if status.Presence != "online" || status.ActivityType != "watching" || status.ActivityText != "tests" {
		t.Fatalf("status request = %#v", status)
	}
}

// --- Helpers ---

func waitForCondition(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition did not become true")
}

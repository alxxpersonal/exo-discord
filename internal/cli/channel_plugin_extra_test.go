package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/hook"
)

// --- Test Cases ---

func TestChannelPluginCommandRejectsDisabledMCP(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = false\n")

	cmd := NewRootCommand(Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	cmd.SetArgs([]string{"channel-plugin"})

	err := cmd.Execute()
	if err == nil || err.Error() != "mcp is disabled" {
		t.Fatalf("Execute() error = %v", err)
	}
}

func TestChannelDispatchHookBehaviors(t *testing.T) {
	t.Parallel()

	response, err := (&channelDispatchHook{}).Decide(context.Background(), hook.Envelope{})
	if err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if response.Decision != hook.DecisionSkip {
		t.Fatalf("decision = %q, want skip", response.Decision)
	}

	wantErr := errors.New("boom")
	_, err = (&channelDispatchHook{
		dispatch: func(context.Context, hook.Envelope) error {
			return wantErr
		},
	}).Decide(context.Background(), hook.Envelope{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Decide() error = %v, want %v", err, wantErr)
	}
}

func TestChannelDispatchHookDrainWaitsForInflightDispatch(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	dispatchHook := &channelDispatchHook{
		dispatch: func(context.Context, hook.Envelope) error {
			close(started)
			<-release
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		_, _ = dispatchHook.Decide(context.Background(), hook.Envelope{})
		close(done)
	}()

	<-started

	drainDone := make(chan int64, 1)
	go func() {
		drainCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		drainDone <- dispatchHook.Drain(drainCtx)
	}()

	select {
	case remaining := <-drainDone:
		t.Fatalf("Drain() returned early with %d remaining", remaining)
	case <-time.After(50 * time.Millisecond):
	}

	close(release)

	select {
	case remaining := <-drainDone:
		if remaining != 0 {
			t.Fatalf("Drain() remaining = %d, want 0", remaining)
		}
	case <-time.After(time.Second):
		t.Fatal("Drain() did not return after dispatch completed")
	}

	<-done
}

func TestChannelDispatchHookDrainReturnsDroppedEventsOnTimeout(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	release := make(chan struct{})
	dispatchHook := &channelDispatchHook{
		dispatch: func(context.Context, hook.Envelope) error {
			close(started)
			<-release
			return nil
		},
	}

	done := make(chan struct{})
	go func() {
		_, _ = dispatchHook.Decide(context.Background(), hook.Envelope{})
		close(done)
	}()

	<-started

	drainCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if remaining := dispatchHook.Drain(drainCtx); remaining != 1 {
		t.Fatalf("Drain() remaining = %d, want 1", remaining)
	}

	close(release)
	<-done
}

func TestEventFromEnvelopeCopiesFields(t *testing.T) {
	t.Parallel()

	event := eventFromEnvelope(hook.Envelope{
		ID:         "evt-1",
		Source:     "discord",
		Mode:       "bot",
		ReceivedAt: time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		Message: hook.MessageEnvelope{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			ChannelType:    "dm",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Attachments: []hook.AttachmentEnvelope{
				{ID: "att-1", Filename: "report.txt", ContentType: "text/plain", SizeBytes: 12},
			},
		},
		Access: hook.AccessEnvelope{
			Reason:             "allowlisted user",
			EffectiveChannelID: "chan-1",
		},
	})

	if event.Message.Attachments[0].Filename != "report.txt" {
		t.Fatalf("attachment filename = %q, want report.txt", event.Message.Attachments[0].Filename)
	}
	if event.Access.Reason != "allowlisted user" {
		t.Fatalf("access reason = %q", event.Access.Reason)
	}
}

func TestIgnoreContextError(t *testing.T) {
	t.Parallel()

	if err := ignoreContextError(context.Canceled); err != nil {
		t.Fatalf("ignoreContextError(context.Canceled) = %v, want nil", err)
	}
	wantErr := errors.New("boom")
	if err := ignoreContextError(wantErr); !errors.Is(err, wantErr) {
		t.Fatalf("ignoreContextError() = %v, want %v", err, wantErr)
	}
}

func TestChannelPluginCommandReturnsManagerOpenError(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\n")

	wantErr := errors.New("manager open failed")
	cmd := NewRootCommand(Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return &fakeSession{}, nil
		},
		NewManager: func(config.ResolvedConfig) (discordpkg.Manager, error) {
			return managerOpenError{err: wantErr}, nil
		},
	})
	cmd.SetArgs([]string{"channel-plugin"})

	err := cmd.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
}

func TestChannelPluginCommandReturnsManagerFactoryError(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\n")

	wantErr := errors.New("manager factory failed")
	cmd := NewRootCommand(Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return &fakeSession{}, nil
		},
		NewManager: func(config.ResolvedConfig) (discordpkg.Manager, error) {
			return nil, wantErr
		},
	})
	cmd.SetArgs([]string{"channel-plugin"})

	err := cmd.Execute()
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
}

func TestChannelPluginCommandReturnsSessionOpenError(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	stdoutReader, stdoutWriter := io.Pipe()

	wantErr := errors.New("open failed")
	cmd := NewRootCommand(Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    stdinReader,
		Stdout:   stdoutWriter,
		Stderr:   &bytes.Buffer{},
		Context: func() context.Context {
			return ctx
		},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return openErrorSession{err: wantErr}, nil
		},
		NewManager: func(config.ResolvedConfig) (discordpkg.Manager, error) {
			return &fakeManager{}, nil
		},
	})
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

	err := <-errCh
	_ = stdinWriter.Close()
	if !errors.Is(err, wantErr) {
		t.Fatalf("Execute() error = %v, want %v", err, wantErr)
	}
}

// --- Test Doubles ---

type managerOpenError struct {
	discordpkg.Manager
	err error
}

func (m managerOpenError) Open(context.Context) error { return m.err }

type openErrorSession struct {
	err error
}

func (o openErrorSession) Open(context.Context) error                 { return o.err }
func (o openErrorSession) Close(context.Context) error                { return nil }
func (o openErrorSession) Mode() string                               { return "bot" }
func (o openErrorSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }
func (o openErrorSession) SendMessage(context.Context, discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, nil
}
func (o openErrorSession) Reply(context.Context, discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, nil
}
func (o openErrorSession) React(context.Context, discordpkg.ReactRequest) error { return nil }
func (o openErrorSession) EditMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, nil
}
func (o openErrorSession) FetchHistory(context.Context, discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	return nil, nil
}
func (o openErrorSession) DownloadAttachments(context.Context, discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	return nil, nil
}
func (o openErrorSession) SetStatus(context.Context, discordpkg.StatusRequest) error { return nil }
func (o openErrorSession) SendTyping(context.Context, string) error                   { return nil }

package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/hook"
)

// --- Test Cases ---

func TestApplyResponseVariants(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	service := newTestService(t, session, nil, &fakeAuditor{}, access.Policy{})
	message := discord.Message{ID: "msg-1", ChannelID: "chan-1"}

	if err := service.applyResponse(context.Background(), message, hook.Response{
		Decision: hook.DecisionReact,
		React:    &hook.ReactAction{Emoji: "👍"},
	}); err != nil {
		t.Fatalf("applyResponse(react) error = %v", err)
	}
	if session.reactRequests[0].MessageID != "msg-1" {
		t.Fatalf("react request = %#v", session.reactRequests[0])
	}

	if err := service.applyResponse(context.Background(), message, hook.Response{
		Decision: hook.DecisionEdit,
		Edit:     &hook.EditAction{MessageID: "msg-edit", Text: "edited"},
	}); err != nil {
		t.Fatalf("applyResponse(edit) error = %v", err)
	}
	if session.editRequests[0].MessageID != "msg-edit" {
		t.Fatalf("edit request = %#v", session.editRequests[0])
	}

	if err := service.applyResponse(context.Background(), message, hook.Response{
		Decision:  hook.DecisionSetStatus,
		SetStatus: &hook.SetStatusAction{Presence: "idle", ActivityType: "watching", ActivityText: "tests"},
	}); err != nil {
		t.Fatalf("applyResponse(set_status) error = %v", err)
	}
	if session.statusCalls[0].Presence != "idle" {
		t.Fatalf("status request = %#v", session.statusCalls[0])
	}
}

func TestRecordSwallowsAuditErrors(t *testing.T) {
	t.Parallel()

	service := New(
		&fakeSession{},
		mustManager(t),
		nil,
		errorAuditor{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		time.Second,
		discord.StatusRequest{},
	)
	service.record(context.Background(), audit.Record{Event: "hook_decision"})
}

func TestUtilityHelpers(t *testing.T) {
	t.Parallel()

	if got, want := firstNonEmpty("", "a", "b"), "a"; got != want {
		t.Fatalf("firstNonEmpty() = %q, want %q", got, want)
	}
	id := newEventID()
	if !strings.HasPrefix(id, "evt_") || len(id) <= 4 {
		t.Fatalf("newEventID() = %q, want evt_ prefix", id)
	}
}

func TestApplyResponseSkipDeferAndUnknown(t *testing.T) {
	t.Parallel()

	service := newTestService(t, &fakeSession{}, nil, &fakeAuditor{}, access.Policy{})
	message := discord.Message{ID: "msg-1", ChannelID: "chan-1"}

	if err := service.applyResponse(context.Background(), message, hook.Response{Decision: hook.DecisionSkip}); err != nil {
		t.Fatalf("applyResponse(skip) error = %v", err)
	}
	if err := service.applyResponse(context.Background(), message, hook.Response{Decision: hook.DecisionDefer}); err != nil {
		t.Fatalf("applyResponse(defer) error = %v", err)
	}
	if err := service.applyResponse(context.Background(), message, hook.Response{Decision: "bad"}); err == nil {
		t.Fatal("applyResponse(bad) error = nil, want error")
	}
}

func TestRunReturnsContextDeadline(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	service := newTestService(t, session, nil, &fakeAuditor{}, access.Policy{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := service.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v", err)
	}
}

// --- Helpers ---

type errorAuditor struct{}

func (errorAuditor) Write(audit.Record) error {
	return errors.New("audit failed")
}

func (f *fakeSession) emit(ctx context.Context, message discord.Message) {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	if handler != nil {
		handler(ctx, message)
	}
}

func mustManager(t *testing.T) *access.Manager {
	t.Helper()

	manager, err := access.NewManager(t.TempDir()+"/access.json", access.Policy{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	return manager
}

func TestRunProcessesInboundMessage(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{
		response: hook.Response{
			Decision: hook.DecisionReact,
			React:    &hook.ReactAction{Emoji: "👍"},
		},
	}
	service := newTestService(t, session, hookClient, &fakeAuditor{}, access.Policy{
		AllowedChannelIDs: []string{"chan-1"},
		RequireMention:    true,
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		ready := session.handler != nil
		session.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	session.emit(context.Background(), discord.Message{
		ID:           "msg-1",
		ChannelID:    "chan-1",
		ChannelKind:  discord.ChannelKindGuildText,
		AuthorID:     "user-1",
		MentionedBot: true,
	})

	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		session.mu.Lock()
		doneReact := len(session.reactRequests) == 1
		session.mu.Unlock()
		if doneReact {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-done; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}

	if len(session.reactRequests) != 1 {
		t.Fatalf("react requests = %d, want 1", len(session.reactRequests))
	}
}

package runtime

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/hook"
)

// --- Test Doubles ---

type fakeSession struct {
	mu sync.Mutex

	mode        string
	openCalled  bool
	closeCalled bool

	sendRequests  []discord.SendRequest
	replyRequests []discord.ReplyRequest
	reactRequests []discord.ReactRequest
	editRequests  []discord.EditRequest
	statusCalls   []discord.StatusRequest

	handler discord.InboundHandler
}

func (f *fakeSession) Open(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCalled = true
	return nil
}

func (f *fakeSession) Close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCalled = true
	return nil
}

func (f *fakeSession) Mode() string {
	if f.mode == "" {
		return "bot"
	}
	return f.mode
}

func (f *fakeSession) SendMessage(_ context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendRequests = append(f.sendRequests, req)
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: "send-1", Content: req.Text}, nil
}

func (f *fakeSession) Reply(_ context.Context, req discord.ReplyRequest) (discord.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyRequests = append(f.replyRequests, req)
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: "reply-1", Content: req.Text}, nil
}

func (f *fakeSession) React(_ context.Context, req discord.ReactRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactRequests = append(f.reactRequests, req)
	return nil
}

func (f *fakeSession) EditMessage(_ context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editRequests = append(f.editRequests, req)
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID, Content: req.Text}, nil
}

func (f *fakeSession) FetchHistory(context.Context, discord.HistoryRequest) ([]discord.Message, error) {
	return nil, nil
}

func (f *fakeSession) DownloadAttachments(context.Context, discord.DownloadRequest) ([]discord.DownloadedFile, error) {
	return nil, nil
}

func (f *fakeSession) SendTyping(context.Context, string) error {
	return nil
}

func (f *fakeSession) SetStatus(_ context.Context, req discord.StatusRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusCalls = append(f.statusCalls, req)
	return nil
}

func (f *fakeSession) Subscribe(handler discord.InboundHandler) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = handler
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.handler = nil
	}
}

type fakeHook struct {
	mu        sync.Mutex
	envelopes []hook.Envelope
	response  hook.Response
	err       error
}

func (f *fakeHook) Decide(_ context.Context, envelope hook.Envelope) (hook.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envelopes = append(f.envelopes, envelope)
	return f.response, f.err
}

type fakeAuditor struct {
	mu      sync.Mutex
	records []audit.Record
}

func (f *fakeAuditor) Write(record audit.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, record)
	return nil
}

// --- Test Cases ---

func TestHandleMessageDeniedGuild(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{}
	auditor := &fakeAuditor{}
	service := newTestService(t, session, hookClient, auditor, access.Policy{
		AllowedChannelIDs: []string{"chan-1"},
		RequireMention:    true,
	})

	err := service.HandleMessage(context.Background(), discord.Message{
		ID:          "msg-1",
		ChannelID:   "chan-2",
		ChannelKind: discord.ChannelKindGuildText,
		AuthorID:    "user-1",
	})
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if len(hookClient.envelopes) != 0 {
		t.Fatalf("hook calls = %d, want 0", len(hookClient.envelopes))
	}
	if got := auditor.records[0].Event; got != "message_denied" {
		t.Fatalf("audit event = %q, want message_denied", got)
	}
}

func TestHandleMessagePairingDM(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{}
	auditor := &fakeAuditor{}
	service := newTestService(t, session, hookClient, auditor, access.Policy{
		Pairing: access.PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Hour,
			MaxPending:  8,
			ResendLimit: 2,
		},
	})

	err := service.HandleMessage(context.Background(), discord.Message{
		ID:          "msg-1",
		ChannelID:   "dm-1",
		ChannelKind: discord.ChannelKindDM,
		AuthorID:    "user-1",
	})
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if len(session.sendRequests) != 1 {
		t.Fatalf("pairing messages = %d, want 1", len(session.sendRequests))
	}
	if got := session.sendRequests[0].Text; !strings.Contains(got, "local approval") {
		t.Fatalf("pairing text = %q, want local approval guidance", got)
	}
	if len(hookClient.envelopes) != 0 {
		t.Fatalf("hook calls = %d, want 0", len(hookClient.envelopes))
	}
	if got := auditor.records[0].Event; got != "pair_requested" {
		t.Fatalf("audit event = %q, want pair_requested", got)
	}
}

func TestHandleMessageAllowedReply(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{
		response: hook.Response{
			Decision: hook.DecisionReply,
			Reply: &hook.ReplyAction{
				Text: "hello back",
			},
		},
	}
	auditor := &fakeAuditor{}
	service := newTestService(t, session, hookClient, auditor, access.Policy{
		AllowedChannelIDs: []string{"chan-1"},
		AllowedUserIDs:    []string{"user-1"},
		RequireMention:    true,
	})

	err := service.HandleMessage(context.Background(), discord.Message{
		ID:           "msg-1",
		ChannelID:    "chan-1",
		GuildID:      "guild-1",
		ChannelKind:  discord.ChannelKindGuildText,
		AuthorID:     "user-1",
		MentionedBot: true,
	})
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if len(hookClient.envelopes) != 1 {
		t.Fatalf("hook calls = %d, want 1", len(hookClient.envelopes))
	}
	if len(session.replyRequests) != 1 {
		t.Fatalf("reply calls = %d, want 1", len(session.replyRequests))
	}
	if session.replyRequests[0].ReplyToMessageID != "msg-1" {
		t.Fatalf("reply_to_message_id = %q, want msg-1", session.replyRequests[0].ReplyToMessageID)
	}
	if got := len(auditor.records); got != 2 {
		t.Fatalf("audit records = %d, want 2", got)
	}
	if auditor.records[0].Event != "message_received" || auditor.records[1].Event != "hook_decision" {
		t.Fatalf("audit records = %#v", auditor.records)
	}
}

func TestHandleMessageReplyChunking(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{
		response: hook.Response{
			Decision: hook.DecisionReply,
			Reply: &hook.ReplyAction{
				Text:  strings.Repeat("a", discordMessageLimit+10) + "\n" + strings.Repeat("b", 20),
				Files: []string{"/tmp/spec.txt"},
			},
		},
	}
	service := newTestService(t, session, hookClient, &fakeAuditor{}, access.Policy{
		AllowedChannelIDs: []string{"chan-1"},
		RequireMention:    true,
	})

	err := service.HandleMessage(context.Background(), discord.Message{
		ID:           "msg-1",
		ChannelID:    "chan-1",
		GuildID:      "guild-1",
		ChannelKind:  discord.ChannelKindGuildText,
		AuthorID:     "user-1",
		MentionedBot: true,
	})
	if err != nil {
		t.Fatalf("HandleMessage() error = %v", err)
	}
	if len(session.replyRequests) != 1 {
		t.Fatalf("reply calls = %d, want 1", len(session.replyRequests))
	}
	if len(session.sendRequests) != 1 {
		t.Fatalf("send calls = %d, want 1", len(session.sendRequests))
	}
	if len(session.replyRequests[0].Files) != 1 {
		t.Fatalf("reply files = %#v, want first chunk attachments only", session.replyRequests[0].Files)
	}
}

func TestHandleMessageHookFailureDoesNotCrash(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	hookClient := &fakeHook{err: context.DeadlineExceeded}
	auditor := &fakeAuditor{}
	service := newTestService(t, session, hookClient, auditor, access.Policy{
		AllowedChannelIDs: []string{"chan-1"},
		RequireMention:    true,
	})

	err := service.HandleMessage(context.Background(), discord.Message{
		ID:           "msg-1",
		ChannelID:    "chan-1",
		ChannelKind:  discord.ChannelKindGuildText,
		AuthorID:     "user-1",
		MentionedBot: true,
	})
	if err != nil {
		t.Fatalf("HandleMessage() error = %v, want nil on hook failure", err)
	}
	if got := auditor.records[len(auditor.records)-1].Event; got != "hook_failed" {
		t.Fatalf("audit event = %q, want hook_failed", got)
	}
}

func TestRunOpensSetsStatusAndCloses(t *testing.T) {
	t.Parallel()

	session := &fakeSession{}
	service := newTestService(t, session, nil, &fakeAuditor{}, access.Policy{})
	service.status = discord.StatusRequest{Presence: "online", ActivityText: "tests"}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.openCalled {
		t.Fatal("Open() was not called")
	}
	if !session.closeCalled {
		t.Fatal("Close() was not called")
	}
	if len(session.statusCalls) != 1 {
		t.Fatalf("status calls = %d, want 1", len(session.statusCalls))
	}
}

// --- Helpers ---

func newTestService(t *testing.T, session discord.Session, hookClient hook.Client, auditor Auditor, policy access.Policy) *Service {
	t.Helper()

	manager, err := access.NewManager(t.TempDir()+"/access.json", policy)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	service := New(session, manager, hookClient, auditor, logger, time.Second, discord.StatusRequest{})
	service.now = func() time.Time { return time.Date(2026, 4, 16, 22, 0, 0, 0, time.UTC) }
	service.newEventID = func() string { return "evt-test" }
	return service
}

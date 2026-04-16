package bot

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeClient struct {
	botUserID string

	openCalled  bool
	closeCalled bool

	sendRequest     discord.SendRequest
	replyRequest    discord.ReplyRequest
	reactRequest    discord.ReactRequest
	editRequest     discord.EditRequest
	historyRequest  discord.HistoryRequest
	downloadRequest discord.DownloadRequest
	statusRequest   discord.StatusRequest

	sendResult     discord.SentMessage
	replyResult    discord.SentMessage
	editResult     discord.SentMessage
	historyResult  []discord.Message
	downloadResult []discord.DownloadedFile

	messageHandler func(context.Context, discord.RawMessageEvent)
}

func (f *fakeClient) Open() error {
	f.openCalled = true
	return nil
}

func (f *fakeClient) Close() error {
	f.closeCalled = true
	return nil
}

func (f *fakeClient) BotUserID() string {
	return f.botUserID
}

func (f *fakeClient) OnMessageCreate(handler func(context.Context, discord.RawMessageEvent)) func() {
	f.messageHandler = handler
	return func() {}
}

func (f *fakeClient) SendMessage(_ context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	f.sendRequest = req
	return f.sendResult, nil
}

func (f *fakeClient) Reply(_ context.Context, req discord.ReplyRequest) (discord.SentMessage, error) {
	f.replyRequest = req
	return f.replyResult, nil
}

func (f *fakeClient) React(_ context.Context, req discord.ReactRequest) error {
	f.reactRequest = req
	return nil
}

func (f *fakeClient) EditMessage(_ context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	f.editRequest = req
	return f.editResult, nil
}

func (f *fakeClient) FetchHistory(_ context.Context, req discord.HistoryRequest) ([]discord.Message, error) {
	f.historyRequest = req
	return f.historyResult, nil
}

func (f *fakeClient) DownloadAttachments(_ context.Context, req discord.DownloadRequest) ([]discord.DownloadedFile, error) {
	f.downloadRequest = req
	return f.downloadResult, nil
}

func (f *fakeClient) SetStatus(_ context.Context, req discord.StatusRequest) error {
	f.statusRequest = req
	return nil
}

// --- Test Cases ---

func TestSessionOpenAndClose(t *testing.T) {
	t.Parallel()

	client := &fakeClient{}
	session := NewSession(client)

	if err := session.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !client.openCalled {
		t.Fatal("Open() did not call client.Open()")
	}
	if !client.closeCalled {
		t.Fatal("Close() did not call client.Close()")
	}
}

func TestSessionIgnoresBotAndWebhookMessages(t *testing.T) {
	t.Parallel()

	client := &fakeClient{botUserID: "bot-1"}
	session := NewSession(client)

	called := make(chan struct{}, 1)
	session.Subscribe(func(context.Context, discord.Message) {
		called <- struct{}{}
	})

	client.messageHandler(context.Background(), discord.RawMessageEvent{
		ID:        "1",
		AuthorID:  "bot-1",
		AuthorBot: true,
		ChannelID: "chan",
		Timestamp: time.Now(),
	})
	client.messageHandler(context.Background(), discord.RawMessageEvent{
		ID:        "2",
		AuthorID:  "user-1",
		WebhookID: "webhook-1",
		ChannelID: "chan",
		Timestamp: time.Now(),
	})

	select {
	case <-called:
		t.Fatal("subscriber should not be called")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionNormalizesInboundMessage(t *testing.T) {
	t.Parallel()

	client := &fakeClient{botUserID: "bot-1"}
	session := NewSession(client)

	var (
		got discord.Message
		wg  sync.WaitGroup
	)
	wg.Add(1)
	session.Subscribe(func(_ context.Context, message discord.Message) {
		defer wg.Done()
		got = message
	})

	now := time.Now().UTC()
	client.messageHandler(context.Background(), discord.RawMessageEvent{
		ID:                 "msg-1",
		ChannelID:          "chan-1",
		GuildID:            "guild-1",
		ThreadParentID:     "parent-1",
		ChannelKind:        discord.ChannelKindGuildText,
		AuthorID:           "user-1",
		AuthorUsername:     "testuser",
		RoleIDs:            []string{"role-1", "role-2"},
		Content:            "hello",
		MentionedUserIDs:   []string{"bot-1"},
		ReferencedAuthorID: "bot-1",
		Attachments: []discord.Attachment{
			{ID: "att-1", Filename: "image.png", SizeBytes: 512},
		},
		Timestamp: now,
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive message")
	}

	if got.ID != "msg-1" {
		t.Fatalf("message ID = %q, want msg-1", got.ID)
	}
	if !got.MentionedBot {
		t.Fatal("MentionedBot = false, want true")
	}
	if !got.RepliedToBot {
		t.Fatal("RepliedToBot = false, want true")
	}
	if got.ThreadParentID != "parent-1" {
		t.Fatalf("ThreadParentID = %q, want parent-1", got.ThreadParentID)
	}
	if len(got.RoleIDs) != 2 || got.RoleIDs[0] != "role-1" || got.RoleIDs[1] != "role-2" {
		t.Fatalf("RoleIDs = %#v, want role-1 role-2", got.RoleIDs)
	}
	if len(got.Attachments) != 1 || got.Attachments[0].ID != "att-1" {
		t.Fatalf("attachments = %#v, want att-1", got.Attachments)
	}
	if !got.Timestamp.Equal(now) {
		t.Fatalf("Timestamp = %v, want %v", got.Timestamp, now)
	}
}

func TestSessionUnsubscribe(t *testing.T) {
	t.Parallel()

	client := &fakeClient{botUserID: "bot-1"}
	session := NewSession(client)

	called := make(chan struct{}, 1)
	unsubscribe := session.Subscribe(func(context.Context, discord.Message) {
		called <- struct{}{}
	})
	unsubscribe()

	client.messageHandler(context.Background(), discord.RawMessageEvent{
		ID:        "msg-1",
		AuthorID:  "user-1",
		ChannelID: "chan-1",
		Timestamp: time.Now(),
	})

	select {
	case <-called:
		t.Fatal("subscriber should not be called after unsubscribe")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestSessionPassesThroughActions(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		sendResult:  discord.SentMessage{ChannelID: "chan-1", MessageID: "msg-send"},
		replyResult: discord.SentMessage{ChannelID: "chan-1", MessageID: "msg-reply"},
		editResult:  discord.SentMessage{ChannelID: "chan-1", MessageID: "msg-edit"},
		historyResult: []discord.Message{
			{ID: "hist-1"},
		},
		downloadResult: []discord.DownloadedFile{
			{AttachmentID: "att-1", Path: filepath.Join(t.TempDir(), "att-1.png")},
		},
	}
	session := NewSession(client)
	ctx := context.Background()

	sendResult, err := session.SendMessage(ctx, discord.SendRequest{ChannelID: "chan-1", Text: "hello"})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	replyResult, err := session.Reply(ctx, discord.ReplyRequest{ChannelID: "chan-1", ReplyToMessageID: "msg-1", Text: "reply"})
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}
	if err := session.React(ctx, discord.ReactRequest{ChannelID: "chan-1", MessageID: "msg-1", Emoji: "👍"}); err != nil {
		t.Fatalf("React() error = %v", err)
	}
	editResult, err := session.EditMessage(ctx, discord.EditRequest{ChannelID: "chan-1", MessageID: "msg-1", Text: "edit"})
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}
	historyResult, err := session.FetchHistory(ctx, discord.HistoryRequest{ChannelID: "chan-1", Limit: 10})
	if err != nil {
		t.Fatalf("FetchHistory() error = %v", err)
	}
	downloadResult, err := session.DownloadAttachments(ctx, discord.DownloadRequest{
		ChannelID:      "chan-1",
		MessageID:      "msg-1",
		DestinationDir: t.TempDir(),
		MaxBytes:       1024,
	})
	if err != nil {
		t.Fatalf("DownloadAttachments() error = %v", err)
	}
	if err := session.SetStatus(ctx, discord.StatusRequest{Presence: "online", ActivityType: "watching", ActivityText: "tests"}); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}

	if sendResult.MessageID != "msg-send" {
		t.Fatalf("SendMessage() = %#v, want msg-send", sendResult)
	}
	if replyResult.MessageID != "msg-reply" {
		t.Fatalf("Reply() = %#v, want msg-reply", replyResult)
	}
	if editResult.MessageID != "msg-edit" {
		t.Fatalf("EditMessage() = %#v, want msg-edit", editResult)
	}
	if len(historyResult) != 1 || historyResult[0].ID != "hist-1" {
		t.Fatalf("FetchHistory() = %#v, want hist-1", historyResult)
	}
	if len(downloadResult) != 1 || downloadResult[0].AttachmentID != "att-1" {
		t.Fatalf("DownloadAttachments() = %#v, want att-1", downloadResult)
	}
	if client.reactRequest.Emoji != "👍" {
		t.Fatalf("React request = %#v, want emoji 👍", client.reactRequest)
	}
	if client.statusRequest.ActivityText != "tests" {
		t.Fatalf("status request = %#v, want tests", client.statusRequest)
	}
}

package mcp

import (
	"context"
	"errors"
	"testing"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Test Doubles ---

type fakeSession struct {
	sendRequest     discordpkg.SendRequest
	replyRequest    discordpkg.ReplyRequest
	reactRequest    discordpkg.ReactRequest
	editRequest     discordpkg.EditRequest
	historyRequest  discordpkg.HistoryRequest
	downloadRequest discordpkg.DownloadRequest
	statusRequest   discordpkg.StatusRequest
}

type errorSession struct {
	err error
}

func (e *errorSession) Open(context.Context) error                 { return nil }
func (e *errorSession) Close(context.Context) error                { return nil }
func (e *errorSession) Mode() string                               { return "bot" }
func (e *errorSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }
func (e *errorSession) SendMessage(context.Context, discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) Reply(context.Context, discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) React(context.Context, discordpkg.ReactRequest) error { return e.err }
func (e *errorSession) EditMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) FetchHistory(context.Context, discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	return nil, e.err
}
func (e *errorSession) DownloadAttachments(context.Context, discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	return nil, e.err
}
func (e *errorSession) SetStatus(context.Context, discordpkg.StatusRequest) error { return e.err }

func (f *fakeSession) Open(context.Context) error                 { return nil }
func (f *fakeSession) Close(context.Context) error                { return nil }
func (f *fakeSession) Mode() string                               { return "bot" }
func (f *fakeSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }

func (f *fakeSession) SendMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	f.sendRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "send-1"}, nil
}

func (f *fakeSession) Reply(_ context.Context, req discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	f.replyRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "reply-1"}, nil
}

func (f *fakeSession) React(_ context.Context, req discordpkg.ReactRequest) error {
	f.reactRequest = req
	return nil
}

func (f *fakeSession) EditMessage(_ context.Context, req discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	f.editRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) FetchHistory(_ context.Context, req discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	f.historyRequest = req
	return []discordpkg.Message{{ID: "hist-1"}}, nil
}

func (f *fakeSession) DownloadAttachments(_ context.Context, req discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	f.downloadRequest = req
	return []discordpkg.DownloadedFile{{AttachmentID: "att-1", Path: req.DestinationDir + "/att-1.png"}}, nil
}

func (f *fakeSession) SetStatus(_ context.Context, req discordpkg.StatusRequest) error {
	f.statusRequest = req
	return nil
}

// --- Test Cases ---

func TestServerListTools(t *testing.T) {
	t.Parallel()

	server, clientSession := connectTestServer(t, &fakeSession{})
	t.Cleanup(func() {
		_ = clientSession.Close()
	})
	_ = server

	result, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	if got, want := len(result.Tools), 7; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}
}

func TestServerRaw(t *testing.T) {
	t.Parallel()

	server := NewServer(&fakeSession{})
	if server.Raw() == nil {
		t.Fatal("Raw() = nil, want server")
	}
}

func TestServerCallTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tool  string
		args  map[string]any
		check func(t *testing.T, session *fakeSession)
	}{
		{
			name: "send",
			tool: "send_message",
			args: map[string]any{"channel_id": "chan-1", "text": "hello"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.sendRequest.ChannelID != "chan-1" || session.sendRequest.Text != "hello" {
					t.Fatalf("send request = %#v", session.sendRequest)
				}
			},
		},
		{
			name: "reply",
			tool: "reply",
			args: map[string]any{"channel_id": "chan-1", "reply_to_message_id": "msg-1", "text": "reply"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.replyRequest.ReplyToMessageID != "msg-1" {
					t.Fatalf("reply request = %#v", session.replyRequest)
				}
			},
		},
		{
			name: "react",
			tool: "react",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "emoji": "👍"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.reactRequest.Emoji != "👍" {
					t.Fatalf("react request = %#v", session.reactRequest)
				}
			},
		},
		{
			name: "fetch-history",
			tool: "fetch_history",
			args: map[string]any{"channel_id": "chan-1", "limit": 5},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.historyRequest.Limit != 5 {
					t.Fatalf("history request = %#v", session.historyRequest)
				}
			},
		},
		{
			name: "download",
			tool: "download_attachment",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "destination_dir": "/tmp/inbox", "max_bytes": 2048},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.downloadRequest.DestinationDir != "/tmp/inbox" || session.downloadRequest.MaxBytes != 2048 {
					t.Fatalf("download request = %#v", session.downloadRequest)
				}
			},
		},
		{
			name: "edit",
			tool: "edit_message",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "text": "edited"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.editRequest.Text != "edited" {
					t.Fatalf("edit request = %#v", session.editRequest)
				}
			},
		},
		{
			name: "status",
			tool: "set_status",
			args: map[string]any{"presence": "online", "activity_type": "watching", "activity_text": "tests"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.statusRequest.ActivityText != "tests" {
					t.Fatalf("status request = %#v", session.statusRequest)
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			session := &fakeSession{}
			server, clientSession := connectTestServer(t, session)
			t.Cleanup(func() {
				_ = clientSession.Close()
			})
			_ = server

			result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      test.tool,
				Arguments: test.args,
			})
			if err != nil {
				t.Fatalf("CallTool(%s) error = %v", test.tool, err)
			}
			if result.IsError {
				t.Fatalf("CallTool(%s) returned MCP error", test.tool)
			}
			test.check(t, session)
		})
	}
}

func TestServerCallToolErrors(t *testing.T) {
	t.Parallel()

	server, clientSession := connectTestServer(t, &errorSession{err: errors.New("boom")})
	t.Cleanup(func() {
		_ = clientSession.Close()
	})
	_ = server

	result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "send_message",
		Arguments: map[string]any{"channel_id": "chan-1", "text": "hello"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("CallTool() IsError = false, want true")
	}
}

// --- Helpers ---

func connectTestServer(t *testing.T, session discordpkg.Session) (*Server, *sdkmcp.ClientSession) {
	t.Helper()

	server := NewServer(session)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx := context.Background()
	go func() {
		_ = server.Raw().Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	return server, clientSession
}

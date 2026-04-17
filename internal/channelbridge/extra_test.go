package channelbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/gorilla/websocket"
)

// --- Test Doubles ---

type fakeAdapter struct {
	name       string
	deliverErr error
	closeErr   error
	delivered  int
	closed     int
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) Deliver(context.Context, Event) error {
	f.delivered++
	return f.deliverErr
}

func (f *fakeAdapter) Close() error {
	f.closed++
	return f.closeErr
}

type fakeCodexConnection struct {
	writes [][]byte
	err    error
	closed bool
}

func (f *fakeCodexConnection) ReadJSON(context.Context) ([]byte, error) {
	return nil, io.EOF
}

func (f *fakeCodexConnection) WriteJSON(_ context.Context, payload []byte) error {
	if f.err != nil {
		return f.err
	}
	f.writes = append(f.writes, append([]byte(nil), payload...))
	return nil
}

func (f *fakeCodexConnection) Close() error {
	f.closed = true
	return nil
}

type mirrorSession struct {
	mu     sync.Mutex
	replys []discordpkg.ReplyRequest
	sends  []discordpkg.SendRequest
}

func (m *mirrorSession) Open(context.Context) error                 { return nil }
func (m *mirrorSession) Close(context.Context) error                { return nil }
func (m *mirrorSession) Mode() string                               { return "bot" }
func (m *mirrorSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }
func (m *mirrorSession) React(context.Context, discordpkg.ReactRequest) error {
	return nil
}
func (m *mirrorSession) EditMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, nil
}
func (m *mirrorSession) FetchHistory(context.Context, discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	return nil, nil
}
func (m *mirrorSession) DownloadAttachments(context.Context, discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	return nil, nil
}
func (m *mirrorSession) SetStatus(context.Context, discordpkg.StatusRequest) error { return nil }
func (m *mirrorSession) SendMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sends = append(m.sends, req)
	return discordpkg.SentMessage{}, nil
}
func (m *mirrorSession) Reply(_ context.Context, req discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replys = append(m.replys, req)
	return discordpkg.SentMessage{}, nil
}

func (m *mirrorSession) replyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.replys)
}

func (m *mirrorSession) sendCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sends)
}

func (m *mirrorSession) firstReply() discordpkg.ReplyRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replys[0]
}

type fakeThreadProvider struct {
	threads []codexThreadSummary
	err     error
}

func (f fakeThreadProvider) ListThreads(context.Context) ([]codexThreadSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]codexThreadSummary(nil), f.threads...), nil
}

type scriptedCodexConnection struct {
	reads  [][]byte
	writes [][]byte
	index  int
}

func (s *scriptedCodexConnection) ReadJSON(context.Context) ([]byte, error) {
	if s.index >= len(s.reads) {
		return nil, io.EOF
	}
	value := s.reads[s.index]
	s.index++
	return value, nil
}

func (s *scriptedCodexConnection) WriteJSON(_ context.Context, payload []byte) error {
	s.writes = append(s.writes, append([]byte(nil), payload...))
	return nil
}

func (s *scriptedCodexConnection) Close() error { return nil }

// --- Test Cases ---

func TestMultiAdapterDeliverAndClose(t *testing.T) {
	t.Parallel()

	first := &fakeAdapter{name: "first"}
	second := &fakeAdapter{name: "second"}
	adapter := multiAdapter{first, second}

	if err := adapter.Deliver(context.Background(), Event{}); err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}
	if first.delivered != 1 || second.delivered != 1 {
		t.Fatalf("delivered counts = %d/%d, want 1/1", first.delivered, second.delivered)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if first.closed != 1 || second.closed != 1 {
		t.Fatalf("closed counts = %d/%d, want 1/1", first.closed, second.closed)
	}
}

func TestMultiAdapterStopsOnDeliverError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	first := &fakeAdapter{name: "first", deliverErr: wantErr}
	second := &fakeAdapter{name: "second"}
	adapter := multiAdapter{first, second}

	err := adapter.Deliver(context.Background(), Event{})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Deliver() error = %v, want %v", err, wantErr)
	}
	if second.delivered != 0 {
		t.Fatalf("second adapter delivered = %d, want 0", second.delivered)
	}
}

func TestClaudeAdapterCloseReturnsNil(t *testing.T) {
	t.Parallel()

	if err := NewClaudeAdapter(io.Discard, nil).Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestClaudeAdapterSendChannelNotificationRequiresWriter(t *testing.T) {
	t.Parallel()

	err := (&ClaudeAdapter{}).SendChannelNotification("hello", nil)
	if err == nil {
		t.Fatal("SendChannelNotification() error = nil, want error")
	}
}

func TestCodexAdapterResolveThreadIDPaths(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadProvider{
		threads: []codexThreadSummary{{ID: "thread-active", Status: "active"}},
	})
	adapter := &CodexAdapter{
		threadID:    "",
		threadStore: store,
	}

	id, explicit, err := adapter.resolveThreadID(context.Background())
	if err != nil {
		t.Fatalf("resolveThreadID() error = %v", err)
	}
	if id != "thread-active" || explicit {
		t.Fatalf("resolveThreadID() = %q, %t", id, explicit)
	}

	if err := store.SaveThread("thread-saved"); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}
	id, explicit, err = adapter.resolveThreadID(context.Background())
	if err != nil {
		t.Fatalf("resolveThreadID() error = %v", err)
	}
	if id != "thread-saved" || explicit {
		t.Fatalf("resolveThreadID() = %q, %t", id, explicit)
	}

	adapter.threadID = "thread-explicit"
	id, explicit, err = adapter.resolveThreadID(context.Background())
	if err != nil {
		t.Fatalf("resolveThreadID() error = %v", err)
	}
	if id != "thread-explicit" || !explicit {
		t.Fatalf("resolveThreadID() = %q, %t", id, explicit)
	}
}

func TestCodexAdapterHandleServerRequestWritesMethodNotFound(t *testing.T) {
	t.Parallel()

	conn := &fakeCodexConnection{}
	adapter := &CodexAdapter{conn: conn}
	adapter.handleServerRequest(context.Background(), "item/tool/requestUserInput", json.RawMessage("7"))

	if len(conn.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(conn.writes))
	}

	var response map[string]any
	if err := json.Unmarshal(conn.writes[0], &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if response["id"].(float64) != 7 {
		t.Fatalf("response id = %#v, want 7", response["id"])
	}
}

func TestCodexAdapterReadLoopDispatchesResponsesAndNotifications(t *testing.T) {
	t.Parallel()

	responsePayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"ok": true,
		},
	})
	if err != nil {
		t.Fatalf("Marshal(response) error = %v", err)
	}
	serverRequestPayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "item/tool/requestUserInput",
		"params":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("Marshal(request) error = %v", err)
	}
	notificationPayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "turn/completed",
		"params": map[string]any{
			"turn": map[string]any{
				"id": "turn-1",
				"items": []map[string]any{
					{
						"type": "agentMessage",
						"text": "done",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(notification) error = %v", err)
	}

	conn := &scriptedCodexConnection{
		reads: [][]byte{responsePayload, serverRequestPayload, notificationPayload},
	}
	session := &mirrorSession{}
	responseCh := make(chan codexResponseMessage, 1)
	adapter := &CodexAdapter{
		conn:            conn,
		pending:         map[int64]chan codexResponseMessage{1: responseCh},
		mirrorResponses: true,
		session:         session,
		turns: map[string]codexMirrorTarget{
			"turn-1": {ChannelID: "chan-1", MessageID: "msg-1"},
		},
	}

	adapter.readLoop(context.Background())

	select {
	case response := <-responseCh:
		if response.ID != 1 {
			t.Fatalf("response id = %d, want 1", response.ID)
		}
	default:
		t.Fatal("response channel did not receive a value")
	}

	if len(conn.writes) != 1 {
		t.Fatalf("server request responses = %d, want 1", len(conn.writes))
	}
	if session.replyCount() != 1 {
		t.Fatalf("reply count = %d, want 1", session.replyCount())
	}
}

func TestCodexAdapterHandleNotificationMirrorsReply(t *testing.T) {
	t.Parallel()

	session := &mirrorSession{}
	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         session,
		turns: map[string]codexMirrorTarget{
			"turn-1": {
				ChannelID: "chan-1",
				GuildID:   "guild-1",
				MessageID: "msg-1",
			},
		},
	}

	longText := strings.Repeat("a", 2105)
	payload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-1",
		Turn: codexTurn{
			ID: "turn-1",
			Items: []codexTurnItem{
				{Type: "agentMessage", Text: longText},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	adapter.handleNotification(codexNotificationMessage{
		Method: "turn/completed",
		Params: payload,
	})

	if session.replyCount() != 1 {
		t.Fatalf("reply count = %d, want 1", session.replyCount())
	}
	if session.sendCount() != 1 {
		t.Fatalf("send count = %d, want 1", session.sendCount())
	}
	if got := len([]rune(session.firstReply().Text)); got != 2000 {
		t.Fatalf("reply chunk len = %d, want 2000", got)
	}
}

func TestCodexAdapterHandleNotificationTurnFailedClearsPendingTurn(t *testing.T) {
	t.Parallel()

	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         &mirrorSession{},
		turns: map[string]codexMirrorTarget{
			"turn-1": {ChannelID: "chan-1", MessageID: "msg-1"},
		},
	}

	payload, err := json.Marshal(map[string]any{
		"turn": map[string]any{
			"id": "turn-1",
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	adapter.handleNotification(codexNotificationMessage{
		Method: "turn/failed",
		Params: payload,
	})

	if _, ok := adapter.turns["turn-1"]; ok {
		t.Fatal("turn target still present after turn/failed")
	}
}

func TestCodexAdapterRequestWriteError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("write failed")
	adapter := &CodexAdapter{
		conn:    &fakeCodexConnection{err: wantErr},
		pending: make(map[int64]chan codexResponseMessage),
	}

	err := adapter.request(context.Background(), "thread/list", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("request() error = %v, want write failure", err)
	}
}

func TestCodexAdapterEnsureConnectedReconnectsAfterInitializeFailure(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-initialize-reset-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	var acceptCount int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			attempt := atomic.AddInt32(&acceptCount, 1)
			go func(attempt int32, conn net.Conn) {
				defer func() {
					_ = conn.Close()
				}()

				reader := bufio.NewReader(conn)
				switch attempt {
				case 1:
					line, readErr := reader.ReadBytes('\n')
					if readErr != nil {
						return
					}

					var request map[string]any
					if err := json.Unmarshal(line, &request); err != nil {
						t.Errorf("Unmarshal() error = %v", err)
						return
					}
					if method := request["method"]; method != "initialize" {
						t.Errorf("attempt 1 method = %#v, want initialize", method)
						return
					}
					writeUnixError(conn, request["id"], "initialize rejected")
				case 2:
					for step := 0; step < 3; step++ {
						line, readErr := reader.ReadBytes('\n')
						if readErr != nil {
							return
						}

						var request map[string]any
						if err := json.Unmarshal(line, &request); err != nil {
							t.Errorf("Unmarshal() error = %v", err)
							return
						}

						method := request["method"].(string)
						switch step {
						case 0:
							if method != "initialize" {
								t.Errorf("step 0 method = %q, want initialize", method)
								return
							}
							writeUnixResponse(conn, request["id"], map[string]any{})
						case 1:
							if method != "initialized" {
								t.Errorf("step 1 method = %q, want initialized", method)
								return
							}
						case 2:
							if method != "turn/start" {
								t.Errorf("step 2 method = %q, want turn/start", method)
								return
							}
							writeUnixResponse(conn, request["id"], map[string]any{
								"turn": map[string]any{
									"id": "turn-1",
								},
							})
						}
					}
				}
			}(attempt, conn)
		}
	}()

	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:  codexTransportUnix,
		SocketPath: socketPath,
		ThreadID:   "thread-1",
	}, HookEnv{
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Timestamp:      time.Now().UTC(),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "initialize codex app-server") {
		t.Fatalf("first Deliver() error = %v, want initialize failure", err)
	}
	if adapter.conn != nil {
		t.Fatal("adapter conn retained after initialize failure")
	}

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-2",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello again",
			Timestamp:      time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("second Deliver() error = %v", err)
	}

	if got := atomic.LoadInt32(&acceptCount); got != 2 {
		t.Fatalf("connect attempts = %d, want 2", got)
	}

	_ = listener.Close()
	<-done
}

func TestCodexAdapterCloseClosesConnection(t *testing.T) {
	t.Parallel()

	conn := &fakeCodexConnection{}
	adapter := &CodexAdapter{conn: conn}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if !conn.closed {
		t.Fatal("connection was not closed")
	}
}

func TestNewCodexAdapterRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	_, err := NewCodexAdapter(CodexConfig{Transport: codexTransportUnix}, HookEnv{HomeDir: t.TempDir()})
	if err == nil {
		t.Fatal("NewCodexAdapter() error = nil, want unix socket error")
	}

	_, err = NewCodexAdapter(CodexConfig{Transport: codexTransportWS}, HookEnv{HomeDir: t.TempDir()})
	if err == nil {
		t.Fatal("NewCodexAdapter() error = nil, want websocket url error")
	}
}

func TestCodexThreadStoreDiscoverActiveThreadErrorsOnEmptyList(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadProvider{})
	_, err := store.DiscoverActiveThread(context.Background())
	if err == nil {
		t.Fatal("DiscoverActiveThread() error = nil, want error")
	}
}

func TestCodexAdapterListThreadsSortsByUpdatedAt(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-list-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()

		reader := bufio.NewReader(conn)
		for step := 0; step < 3; step++ {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				return
			}
			var request map[string]any
			if err := json.Unmarshal(line, &request); err != nil {
				return
			}

			switch request["method"] {
			case "initialize":
				writeUnixResponse(conn, request["id"], map[string]any{})
			case "thread/list":
				writeUnixResponse(conn, request["id"], map[string]any{
					"data": []map[string]any{
						{
							"id":        "thread-old",
							"updatedAt": 1,
							"status": map[string]any{
								"type": "idle",
							},
						},
						{
							"id":        "thread-new",
							"updatedAt": 5,
							"status": map[string]any{
								"type": "active",
							},
						},
					},
				})
			}
		}
	}()

	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:  codexTransportUnix,
		SocketPath: socketPath,
	}, HookEnv{
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}

	threads, err := adapter.ListThreads(context.Background())
	if err != nil {
		t.Fatalf("ListThreads() error = %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("thread count = %d, want 2", len(threads))
	}
	if threads[0].ID != "thread-new" {
		t.Fatalf("first thread = %q, want thread-new", threads[0].ID)
	}
}

func TestIsThreadNotFoundError(t *testing.T) {
	t.Parallel()

	if !isThreadNotFoundError(errors.New("turn/start failed: thread not found")) {
		t.Fatal("isThreadNotFoundError() = false, want true")
	}
	if isThreadNotFoundError(nil) {
		t.Fatal("isThreadNotFoundError(nil) = true, want false")
	}
}

func TestCodexAdapterDeliverRetriesSavedThreadAndMirrorsResponse(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-retry-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	done := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
			close(done)
		}()

		reader := bufio.NewReader(conn)
		for step := 0; step < 5; step++ {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				return
			}
			var request map[string]any
			if err := json.Unmarshal(line, &request); err != nil {
				t.Errorf("Unmarshal() error = %v", err)
				return
			}

			method := request["method"].(string)
			switch step {
			case 0:
				if method != "initialize" {
					t.Errorf("step 0 method = %q, want initialize", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{})
			case 1:
				if method != "initialized" {
					t.Errorf("step 1 method = %q, want initialized", method)
					return
				}
			case 2:
				if method != "turn/start" {
					t.Errorf("step 2 method = %q, want turn/start", method)
					return
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 3:
				if method != "thread/list" {
					t.Errorf("step 3 method = %q, want thread/list", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"data": []map[string]any{
						{
							"id":        "thread-new",
							"updatedAt": 2,
							"status": map[string]any{
								"type": "active",
							},
						},
					},
				})
			case 4:
				if method != "turn/start" {
					t.Errorf("step 4 method = %q, want turn/start", method)
					return
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-new" {
					t.Errorf("threadId = %#v, want thread-new", params["threadId"])
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"turn": map[string]any{
						"id": "turn-2",
					},
				})
				time.Sleep(25 * time.Millisecond)
				notification, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"method":  "turn/completed",
					"params": map[string]any{
						"threadId": "thread-new",
						"turn": map[string]any{
							"id": "turn-2",
							"items": []map[string]any{
								{
									"type": "agentMessage",
									"text": "mirrored reply",
								},
							},
						},
					},
				})
				_, _ = conn.Write(append(notification, '\n'))
			}
		}
	}()

	session := &mirrorSession{}
	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:       codexTransportUnix,
		SocketPath:      socketPath,
		MirrorResponses: true,
	}, HookEnv{
		HomeDir: t.TempDir(),
		Session: session,
	})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}
	if err := adapter.threadStore.SaveThread("thread-stale"); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			GuildID:        "guild-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Timestamp:      time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		return session.replyCount() == 1
	})
	if session.firstReply().Text != "mirrored reply" {
		t.Fatalf("reply text = %q, want mirrored reply", session.firstReply().Text)
	}

	<-done
}

func TestCodexAdapterDeliverRetriesExplicitThreadAfterRediscovery(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-explicit-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	done := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
			close(done)
		}()

		reader := bufio.NewReader(conn)
		for step := 0; step < 5; step++ {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				return
			}

			var request map[string]any
			if err := json.Unmarshal(line, &request); err != nil {
				t.Errorf("Unmarshal() error = %v", err)
				return
			}

			method := request["method"].(string)
			switch step {
			case 0:
				if method != "initialize" {
					t.Errorf("step 0 method = %q, want initialize", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{})
			case 1:
				if method != "initialized" {
					t.Errorf("step 1 method = %q, want initialized", method)
					return
				}
			case 2:
				if method != "turn/start" {
					t.Errorf("step 2 method = %q, want turn/start", method)
					return
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-stale" {
					t.Errorf("stale threadId = %#v, want thread-stale", params["threadId"])
					return
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 3:
				if method != "thread/list" {
					t.Errorf("step 3 method = %q, want thread/list", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"data": []map[string]any{
						{
							"id":        "thread-new",
							"updatedAt": 3,
							"status": map[string]any{
								"type": "active",
							},
						},
					},
				})
			case 4:
				if method != "turn/start" {
					t.Errorf("step 4 method = %q, want turn/start", method)
					return
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-new" {
					t.Errorf("rediscovered threadId = %#v, want thread-new", params["threadId"])
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"turn": map[string]any{
						"id": "turn-1",
					},
				})
			}
		}
	}()

	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:  codexTransportUnix,
		SocketPath: socketPath,
		ThreadID:   "thread-stale",
	}, HookEnv{
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Timestamp:      time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	if adapter.threadID != "thread-new" {
		t.Fatalf("threadID = %q, want thread-new", adapter.threadID)
	}
	if savedThreadID, ok := adapter.threadStore.LoadThread(); !ok || savedThreadID != "thread-new" {
		t.Fatalf("saved thread = %q, %t, want thread-new, true", savedThreadID, ok)
	}

	<-done
}

func TestCodexAdapterDeliverReturnsDiscoveryErrorForStaleExplicitThread(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-explicit-fail-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	done := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
			close(done)
		}()

		reader := bufio.NewReader(conn)
		for step := 0; step < 3; step++ {
			line, readErr := reader.ReadBytes('\n')
			if readErr != nil {
				return
			}

			var request map[string]any
			if err := json.Unmarshal(line, &request); err != nil {
				t.Errorf("Unmarshal() error = %v", err)
				return
			}

			method := request["method"].(string)
			switch step {
			case 0:
				if method != "initialize" {
					t.Errorf("step 0 method = %q, want initialize", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{})
			case 1:
				if method != "initialized" {
					t.Errorf("step 1 method = %q, want initialized", method)
					return
				}
			case 2:
				if method != "turn/start" {
					t.Errorf("step 2 method = %q, want turn/start", method)
					return
				}
				writeUnixError(conn, request["id"], "thread not found")
			}
		}
	}()

	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:  codexTransportUnix,
		SocketPath: socketPath,
		ThreadID:   "thread-stale",
	}, HookEnv{
		HomeDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}
	adapter.threadStore = NewCodexThreadStore(t.TempDir(), fakeThreadProvider{
		err: errors.New("no active codex threads found"),
	})

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Timestamp:      time.Now().UTC(),
		},
	})
	if err == nil {
		t.Fatal("Deliver() error = nil, want rediscovery failure")
	}
	if !strings.Contains(err.Error(), "rediscover codex thread after thread not found") {
		t.Fatalf("Deliver() error = %v, want rediscovery context", err)
	}
	if !strings.Contains(err.Error(), "no active codex threads found") {
		t.Fatalf("Deliver() error = %v, want discovery failure", err)
	}

	<-done
}

func TestLastCodexAgentMessageAndSplitDiscordChunks(t *testing.T) {
	t.Parallel()

	if got := lastCodexAgentMessage([]codexTurnItem{
		{Type: "userMessage", Text: "ignored"},
		{Type: "agentMessage", Text: "hello"},
	}); got != "hello" {
		t.Fatalf("lastCodexAgentMessage() = %q, want hello", got)
	}

	chunks := splitDiscordChunks(strings.Repeat("b", 2100), 2000)
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
}

func TestFormatCodexInputIncludesOptionalFields(t *testing.T) {
	t.Parallel()

	text := formatCodexInput(Event{
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			GuildID:        "guild-1",
			ThreadParentID: "parent-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "",
			Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			Attachments: []AttachmentEvent{
				{Filename: "report.txt", ContentType: "text/plain", SizeBytes: 32},
			},
		},
	})

	if !strings.Contains(text, "Guild ID: guild-1") {
		t.Fatalf("formatted text = %q, missing guild id", text)
	}
	if !strings.Contains(text, "Thread Parent ID: parent-1") {
		t.Fatalf("formatted text = %q, missing thread parent id", text)
	}
	if !strings.Contains(text, "report.txt (text/plain, 32 bytes)") {
		t.Fatalf("formatted text = %q, missing attachment line", text)
	}
	if !strings.Contains(text, "Content:\n(empty)") {
		t.Fatalf("formatted text = %q, missing empty content marker", text)
	}
}

func TestConnectionCloseHelpers(t *testing.T) {
	t.Parallel()

	serverConn, clientConn := net.Pipe()
	unixConn := &codexUnixConnection{
		conn:   clientConn,
		reader: bufio.NewReader(clientConn),
	}
	if err := unixConn.Close(); err != nil {
		t.Fatalf("unix Close() error = %v", err)
	}
	_ = serverConn.Close()

	wsServer := websocketTestServer(t)
	defer wsServer.close()
	wsConn, response, err := websocket.DefaultDialer.Dial(wsServer.url, nil)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	websocketConn := &codexWebsocketConnection{conn: wsConn}
	if err := websocketConn.Close(); err != nil {
		t.Fatalf("websocket Close() error = %v", err)
	}
}

// --- Websocket Helper ---

type testWebsocketServer struct {
	url   string
	close func()
}

func websocketTestServer(t *testing.T) testWebsocketServer {
	t.Helper()

	upgrader := websocket.Upgrader{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}

	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		time.Sleep(10 * time.Millisecond)
	})

	go func() {
		_ = server.Serve(listener)
	}()

	return testWebsocketServer{
		url: "ws://" + listener.Addr().String(),
		close: func() {
			_ = server.Close()
			_ = listener.Close()
		},
	}
}

func writeUnixResponse(conn net.Conn, id any, result any) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  result,
	})
	_, _ = conn.Write(append(payload, '\n'))
}

func writeUnixError(conn net.Conn, id any, message string) {
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]any{
			"code":    -32000,
			"message": message,
		},
	})
	_, _ = conn.Write(append(payload, '\n'))
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
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

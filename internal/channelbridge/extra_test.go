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
	"path/filepath"
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

type blockingMirrorSession struct {
	replyStarted chan struct{}
	replyDone    chan struct{}
	replyCalls   chan struct{}
	sendCount    atomic.Int32
	replyCount   atomic.Int32
	startOnce    sync.Once
	doneOnce     sync.Once
}

func (m *blockingMirrorSession) Open(context.Context) error                 { return nil }
func (m *blockingMirrorSession) Close(context.Context) error                { return nil }
func (m *blockingMirrorSession) Mode() string                               { return "bot" }
func (m *blockingMirrorSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }
func (m *blockingMirrorSession) React(context.Context, discordpkg.ReactRequest) error {
	return nil
}
func (m *blockingMirrorSession) EditMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, nil
}
func (m *blockingMirrorSession) FetchHistory(context.Context, discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	return nil, nil
}
func (m *blockingMirrorSession) DownloadAttachments(context.Context, discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	return nil, nil
}
func (m *blockingMirrorSession) SetStatus(context.Context, discordpkg.StatusRequest) error {
	return nil
}
func (m *blockingMirrorSession) SendMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	m.sendCount.Add(1)
	return discordpkg.SentMessage{}, nil
}
func (m *blockingMirrorSession) Reply(ctx context.Context, req discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	m.replyCount.Add(1)
	if m.replyCalls != nil {
		m.replyCalls <- struct{}{}
	}
	m.startOnce.Do(func() {
		close(m.replyStarted)
	})
	<-ctx.Done()
	m.doneOnce.Do(func() {
		close(m.replyDone)
	})
	return discordpkg.SentMessage{}, ctx.Err()
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

type writeTriggeredCodexConnection struct {
	scriptedCodexConnection
	readGate  chan struct{}
	writeOnce sync.Once
}

func (s *writeTriggeredCodexConnection) ReadJSON(ctx context.Context) ([]byte, error) {
	if s.readGate != nil {
		select {
		case <-s.readGate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.scriptedCodexConnection.ReadJSON(ctx)
}

func (s *writeTriggeredCodexConnection) WriteJSON(ctx context.Context, payload []byte) error {
	if err := s.scriptedCodexConnection.WriteJSON(ctx, payload); err != nil {
		return err
	}
	s.writeOnce.Do(func() {
		close(s.readGate)
	})
	return nil
}

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

	id, explicit, _, err := adapter.resolveThreadID(context.Background())
	if err != nil {
		t.Fatalf("resolveThreadID() error = %v", err)
	}
	if id != "thread-active" || explicit {
		t.Fatalf("resolveThreadID() = %q, %t", id, explicit)
	}

	if err := store.SaveThread("thread-saved", codexThreadOriginCached); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}
	id, explicit, _, err = adapter.resolveThreadID(context.Background())
	if err != nil {
		t.Fatalf("resolveThreadID() error = %v", err)
	}
	if id != "thread-saved" || explicit {
		t.Fatalf("resolveThreadID() = %q, %t", id, explicit)
	}

	adapter.threadID = "thread-explicit"
	id, explicit, _, err = adapter.resolveThreadID(context.Background())
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
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-1": {ChannelID: "chan-1", MessageID: "msg-1"},
		},
	}

	adapter.readLoop(context.Background())

	// mirror dispatch is async, wait for the goroutine to land.
	waitUntil(t, time.Second, func() bool {
		return session.replyCount() == 1
	})
	adapter.mirrorWG.Wait()

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
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-1": {
				ChannelID: "chan-1",
				GuildID:   "guild-1",
				MessageID: "msg-1",
			},
		},
	}

	// legacy shape: items inside the turn/completed payload itself. real
	// codex servers never emit this, but the fallback path is kept for
	// robustness against protocol churn.
	longText := strings.Repeat("a", 2105)
	payload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-1",
		Turn: codexTurn{
			ID: "turn-1",
			Items: []codexThreadItem{
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

	adapter.mirrorWG.Wait()

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

func TestCodexAdapterHandleNotificationStopsReplyWhenClosed(t *testing.T) {
	t.Parallel()

	session := &blockingMirrorSession{
		replyStarted: make(chan struct{}),
		replyDone:    make(chan struct{}),
	}
	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         session,
		serviceCtx:      serviceCtx,
		serviceCancel:   serviceCancel,
		closeCh:         make(chan struct{}),
		turns: map[string]codexMirrorTarget{
			"turn-1": {
				ChannelID: "chan-1",
				GuildID:   "guild-1",
				MessageID: "msg-1",
			},
		},
	}

	payload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-1",
		Turn: codexTurn{
			ID: "turn-1",
			Items: []codexThreadItem{
				{Type: "agentMessage", Text: strings.Repeat("a", 2105)},
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		adapter.handleNotification(codexNotificationMessage{
			Method: "turn/completed",
			Params: payload,
		})
		close(done)
	}()

	<-session.replyStarted
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-session.replyDone:
	case <-time.After(time.Second):
		t.Fatal("reply did not stop after Close()")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("handleNotification did not return after Close()")
	}

	if got := session.sendCount.Load(); got != 0 {
		t.Fatalf("send count = %d, want 0", got)
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

func TestCodexAdapterInitializeConnectionSendsHandshake(t *testing.T) {
	t.Parallel()

	response, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]any{},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	conn := &scriptedCodexConnection{
		reads: [][]byte{response},
	}
	adapter := &CodexAdapter{}
	if err := adapter.initializeConnection(context.Background(), conn); err != nil {
		t.Fatalf("initializeConnection() error = %v", err)
	}

	if len(conn.writes) != 2 {
		t.Fatalf("writes = %d, want 2", len(conn.writes))
	}

	var initializeRequest codexRequestMessage
	if err := json.Unmarshal(conn.writes[0], &initializeRequest); err != nil {
		t.Fatalf("Unmarshal(initialize) error = %v", err)
	}
	if initializeRequest.Method != "initialize" {
		t.Fatalf("first method = %q, want initialize", initializeRequest.Method)
	}

	var initializedRequest codexRequestMessage
	if err := json.Unmarshal(conn.writes[1], &initializedRequest); err != nil {
		t.Fatalf("Unmarshal(initialized) error = %v", err)
	}
	if initializedRequest.Method != "initialized" {
		t.Fatalf("second method = %q, want initialized", initializedRequest.Method)
	}
}

func TestCodexAdapterNotifyWritesToActiveConnection(t *testing.T) {
	t.Parallel()

	conn := &fakeCodexConnection{}
	adapter := &CodexAdapter{conn: conn}
	if err := adapter.notify(context.Background(), "initialized", codexInitializedParams{}); err != nil {
		t.Fatalf("notify() error = %v", err)
	}

	if len(conn.writes) != 1 {
		t.Fatalf("writes = %d, want 1", len(conn.writes))
	}

	var request codexRequestMessage
	if err := json.Unmarshal(conn.writes[0], &request); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if request.Method != "initialized" {
		t.Fatalf("method = %q, want initialized", request.Method)
	}
}

func TestCodexAdapterWaitForReconnectDelayTimer(t *testing.T) {
	t.Parallel()

	adapter := &CodexAdapter{closeCh: make(chan struct{})}
	if err := adapter.waitForReconnectDelay(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("waitForReconnectDelay() error = %v", err)
	}
}

func TestCodexAdapterWaitForReconnectDelayReturnsCanceledWhenClosed(t *testing.T) {
	t.Parallel()

	closeCh := make(chan struct{})
	close(closeCh)
	adapter := &CodexAdapter{closeCh: closeCh}

	if err := adapter.waitForReconnectDelay(context.Background(), 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForReconnectDelay(0) error = %v, want context.Canceled", err)
	}
	if err := adapter.waitForReconnectDelay(context.Background(), time.Millisecond); !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForReconnectDelay(timer) error = %v, want context.Canceled", err)
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

func TestCodexAdapterEnsureConnectedReconnectsAfterReadLoopError(t *testing.T) {
	t.Parallel()

	socketPath := "/tmp/exo-discord-read-reconnect-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".sock"
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
							t.Errorf("attempt %d step 0 method = %q, want initialize", attempt, method)
							return
						}
						writeUnixResponse(conn, request["id"], map[string]any{})
					case 1:
						if method != "initialized" {
							t.Errorf("attempt %d step 1 method = %q, want initialized", attempt, method)
							return
						}
					case 2:
						if method != "turn/start" {
							t.Errorf("attempt %d step 2 method = %q, want turn/start", attempt, method)
							return
						}
						writeUnixResponse(conn, request["id"], map[string]any{
							"turn": map[string]any{
								"id": "turn-" + strconv.FormatInt(int64(attempt), 10),
							},
						})
						return
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
	adapter.reconnectBackoff = []time.Duration{0}

	firstEvent := Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello",
			Timestamp:      time.Now().UTC(),
		},
	}
	if err := adapter.Deliver(context.Background(), firstEvent); err != nil {
		t.Fatalf("first Deliver() error = %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		adapter.mu.Lock()
		defer adapter.mu.Unlock()
		return adapter.conn == nil && adapter.closeErr != nil
	})

	secondEvent := Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-2",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello again",
			Timestamp:      time.Now().UTC(),
		},
	}
	if err := adapter.Deliver(context.Background(), secondEvent); err != nil {
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

func TestCodexAdapterDeliverRetriesV1SavedThreadAndMirrorsResponse(t *testing.T) {
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
		for step := 0; step < 6; step++ {
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
				if method != "thread/resume" {
					t.Errorf("step 3 method = %q, want thread/resume", method)
					return
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-stale" {
					t.Errorf("step 3 threadId = %#v, want thread-stale", params["threadId"])
					return
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 4:
				if method != "thread/list" {
					t.Errorf("step 4 method = %q, want thread/list", method)
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
			case 5:
				if method != "turn/start" {
					t.Errorf("step 5 method = %q, want turn/start", method)
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
	if err := os.MkdirAll(filepath.Dir(adapter.threadStore.path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(adapter.threadStore.path, []byte("{\"thread_id\":\"thread-stale\"}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	savedID, savedOrigin, ok := adapter.threadStore.LoadThread()
	if !ok {
		t.Fatal("LoadThread() ok = false, want true")
	}
	if savedID != "thread-stale" {
		t.Fatalf("saved thread id = %q, want thread-stale", savedID)
	}
	if savedOrigin != codexThreadOriginCached {
		t.Fatalf("saved origin = %q, want %q", savedOrigin, codexThreadOriginCached)
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
		for step := 0; step < 6; step++ {
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
				if method != "thread/resume" {
					t.Errorf("step 3 method = %q, want thread/resume", method)
					return
				}
				// resume fails because the stale id is not on disk either
				writeUnixError(conn, request["id"], "thread not found")
			case 4:
				if method != "thread/list" {
					t.Errorf("step 4 method = %q, want thread/list", method)
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
			case 5:
				if method != "turn/start" {
					t.Errorf("step 5 method = %q, want turn/start", method)
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
	if savedThreadID, _, ok := adapter.threadStore.LoadThread(); !ok || savedThreadID != "thread-new" {
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
		for step := 0; step < 4; step++ {
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
				if method != "thread/resume" {
					t.Errorf("step 3 method = %q, want thread/resume", method)
					return
				}
				// resume fails: thread id is stale on disk too
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
	if !strings.Contains(err.Error(), "auto-create is disabled") {
		t.Fatalf("Deliver() error = %v, want auto-create-disabled context", err)
	}
	if !strings.Contains(err.Error(), "no active codex threads found") {
		t.Fatalf("Deliver() error = %v, want discovery failure", err)
	}

	<-done
}

func TestLastCodexAgentMessageAndSplitDiscordChunks(t *testing.T) {
	t.Parallel()

	if got := lastCodexAgentMessage([]codexThreadItem{
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

// --- Mirror Response Regression Tests ---

// TestCodexAdapterMirrorsCompletedTurnReplyBackToDiscord feeds the adapter
// the exact wire shape codex app-server emits after `turn/start`: one or
// more `item/completed` notifications carrying agentMessage text, then a
// `turn/completed` notification whose `turn.items` slice is empty. The
// adapter must reconstruct the reply text from the buffered item stream and
// call Session.Reply with it.
func TestCodexAdapterMirrorsCompletedTurnReplyBackToDiscord(t *testing.T) {
	t.Parallel()

	session := &mirrorSession{}
	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         session,
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-real": {
				ChannelID: "chan-1",
				GuildID:   "guild-1",
				MessageID: "msg-in",
			},
		},
	}

	itemPayload, err := json.Marshal(codexItemCompletedNotification{
		ThreadID: "thread-real",
		TurnID:   "turn-real",
		Item: codexThreadItem{
			Type: "agentMessage",
			ID:   "item-1",
			Text: "hello from codex",
		},
	})
	if err != nil {
		t.Fatalf("Marshal(item) error = %v", err)
	}
	// exact shape codex emits: params carries threadId + turn{id, items:[]}.
	turnPayload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-real",
		Turn:     codexTurn{ID: "turn-real", Items: nil},
	})
	if err != nil {
		t.Fatalf("Marshal(turn) error = %v", err)
	}

	adapter.handleNotification(codexNotificationMessage{Method: "item/completed", Params: itemPayload})
	adapter.handleNotification(codexNotificationMessage{Method: "turn/completed", Params: turnPayload})

	adapter.mirrorWG.Wait()

	if session.replyCount() != 1 {
		t.Fatalf("reply count = %d, want 1", session.replyCount())
	}
	reply := session.firstReply()
	if reply.Text != "hello from codex" {
		t.Fatalf("reply text = %q, want %q", reply.Text, "hello from codex")
	}
	if reply.ReplyToMessageID != "msg-in" {
		t.Fatalf("reply target = %q, want msg-in", reply.ReplyToMessageID)
	}
	if reply.ChannelID != "chan-1" {
		t.Fatalf("reply channel = %q, want chan-1", reply.ChannelID)
	}
}

// TestCodexAdapterDoesNotMirrorWhenFlagDisabled proves that the
// mirror_responses feature flag actually gates the outbound reply. Turning
// it off must short-circuit both item buffering and Reply dispatch.
func TestCodexAdapterDoesNotMirrorWhenFlagDisabled(t *testing.T) {
	t.Parallel()

	session := &mirrorSession{}
	adapter := &CodexAdapter{
		mirrorResponses: false,
		session:         session,
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-real": {ChannelID: "chan-1", MessageID: "msg-in"},
		},
	}

	itemPayload, err := json.Marshal(codexItemCompletedNotification{
		ThreadID: "thread-real",
		TurnID:   "turn-real",
		Item:     codexThreadItem{Type: "agentMessage", ID: "i", Text: "should be ignored"},
	})
	if err != nil {
		t.Fatalf("Marshal(item) error = %v", err)
	}
	turnPayload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-real",
		Turn:     codexTurn{ID: "turn-real"},
	})
	if err != nil {
		t.Fatalf("Marshal(turn) error = %v", err)
	}

	adapter.handleNotification(codexNotificationMessage{Method: "item/completed", Params: itemPayload})
	adapter.handleNotification(codexNotificationMessage{Method: "turn/completed", Params: turnPayload})

	adapter.mirrorWG.Wait()

	if session.replyCount() != 0 {
		t.Fatalf("reply count = %d, want 0 when mirror_responses is off", session.replyCount())
	}
	if session.sendCount() != 0 {
		t.Fatalf("send count = %d, want 0 when mirror_responses is off", session.sendCount())
	}
}

// TestCodexAdapterMirrorChunksLongResponse verifies that a codex reply
// exceeding the discord 2000-char message limit is split across one
// `Reply` plus N-1 follow-up `SendMessage` calls. It also proves multiple
// phase-tagged final_answer item/completed notifications for the same turn
// are joined into one mirrored reply.
func TestCodexAdapterMirrorChunksLongResponse(t *testing.T) {
	t.Parallel()

	session := &mirrorSession{}
	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         session,
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-long": {ChannelID: "chan-1", MessageID: "msg-in"},
		},
	}

	// two items totaling 4004 runes, which spans 3 chunks (2000+2000+4).
	first := strings.Repeat("a", 2000)
	second := strings.Repeat("b", 2000)
	for _, text := range []string{first, second} {
		payload, err := json.Marshal(codexItemCompletedNotification{
			ThreadID: "thread-long",
			TurnID:   "turn-long",
			Item: codexThreadItem{
				Type:  "agentMessage",
				ID:    "i",
				Text:  text,
				Phase: "final_answer",
			},
		})
		if err != nil {
			t.Fatalf("Marshal(item) error = %v", err)
		}
		adapter.handleNotification(codexNotificationMessage{Method: "item/completed", Params: payload})
	}
	turnPayload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-long",
		Turn:     codexTurn{ID: "turn-long"},
	})
	if err != nil {
		t.Fatalf("Marshal(turn) error = %v", err)
	}
	adapter.handleNotification(codexNotificationMessage{Method: "turn/completed", Params: turnPayload})

	adapter.mirrorWG.Wait()

	if session.replyCount() != 1 {
		t.Fatalf("reply count = %d, want 1", session.replyCount())
	}
	// joined text: 2000 + "\n\n" + 2000 = 4002 runes => 3 chunks (2000, 2000, 2).
	if got := session.sendCount(); got != 2 {
		t.Fatalf("send count = %d, want 2 follow-up chunks", got)
	}
	if got := len([]rune(session.firstReply().Text)); got != 2000 {
		t.Fatalf("first chunk len = %d, want 2000", got)
	}
}

// TestCodexAdapterMirrorRespectsShutdown triggers Close() mid-reply and
// asserts the pending mirror dispatch aborts cleanly: the blocking Reply
// unblocks via ctx.Done, no follow-up SendMessage fires, and Close waits
// for the goroutine via the mirror waitgroup before returning.
func TestCodexAdapterMirrorRespectsShutdown(t *testing.T) {
	t.Parallel()

	session := &blockingMirrorSession{
		replyStarted: make(chan struct{}),
		replyDone:    make(chan struct{}),
	}
	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()
	adapter := &CodexAdapter{
		mirrorResponses: true,
		session:         session,
		serviceCtx:      serviceCtx,
		serviceCancel:   serviceCancel,
		closeCh:         make(chan struct{}),
		turnTexts:       make(map[string][]codexThreadItem),
		turns: map[string]codexMirrorTarget{
			"turn-slow": {ChannelID: "chan-1", MessageID: "msg-in"},
		},
	}

	itemPayload, err := json.Marshal(codexItemCompletedNotification{
		ThreadID: "thread-slow",
		TurnID:   "turn-slow",
		Item: codexThreadItem{
			Type: "agentMessage",
			ID:   "i",
			Text: strings.Repeat("z", 2200),
		},
	})
	if err != nil {
		t.Fatalf("Marshal(item) error = %v", err)
	}
	turnPayload, err := json.Marshal(codexTurnCompletedNotification{
		ThreadID: "thread-slow",
		Turn:     codexTurn{ID: "turn-slow"},
	})
	if err != nil {
		t.Fatalf("Marshal(turn) error = %v", err)
	}

	adapter.handleNotification(codexNotificationMessage{Method: "item/completed", Params: itemPayload})
	adapter.handleNotification(codexNotificationMessage{Method: "turn/completed", Params: turnPayload})

	<-session.replyStarted

	closeErrCh := make(chan error, 1)
	go func() { closeErrCh <- adapter.Close() }()

	select {
	case <-session.replyDone:
	case <-time.After(time.Second):
		t.Fatal("reply did not stop after Close()")
	}

	select {
	case err := <-closeErrCh:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not return after mirror goroutine exited")
	}

	if got := session.sendCount.Load(); got != 0 {
		t.Fatalf("send count = %d, want 0", got)
	}
}

func TestCodexAdapterDeliverRegistersMirrorBeforeImmediateCompletion(t *testing.T) {
	t.Parallel()

	responsePayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"turn": map[string]any{
				"id": "turn-fast",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(response) error = %v", err)
	}
	itemPayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "item/completed",
		"params": map[string]any{
			"threadId": "thread-fast",
			"turnId":   "turn-fast",
			"item": map[string]any{
				"type": "agentMessage",
				"id":   "item-fast",
				"text": "fast reply",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(item) error = %v", err)
	}
	turnPayload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "turn/completed",
		"params": map[string]any{
			"threadId": "thread-fast",
			"turn": map[string]any{
				"id": "turn-fast",
			},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(turn) error = %v", err)
	}

	conn := &writeTriggeredCodexConnection{
		scriptedCodexConnection: scriptedCodexConnection{
			reads: [][]byte{responsePayload, itemPayload, turnPayload},
		},
		readGate: make(chan struct{}),
	}
	session := &mirrorSession{}
	adapter := &CodexAdapter{
		conn:            conn,
		threadID:        "thread-fast",
		mirrorResponses: true,
		session:         session,
		pending:         make(map[int64]chan codexResponseMessage),
		pendingMirrors:  make(map[int64]codexMirrorTarget),
		threadStore:     NewCodexThreadStore(t.TempDir(), nil),
		turns:           make(map[string]codexMirrorTarget),
		turnTexts:       make(map[string][]codexThreadItem),
		closeCh:         make(chan struct{}),
	}

	readLoopDone := make(chan struct{})
	go func() {
		adapter.readLoop(context.Background())
		close(readLoopDone)
	}()

	err = adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-fast",
			ChannelID:      "chan-fast",
			AuthorID:       "user-fast",
			AuthorUsername: "alice",
			Content:        "mirror this",
			Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	waitUntil(t, time.Second, func() bool {
		return session.replyCount() == 1
	})
	adapter.mirrorWG.Wait()

	reply := session.firstReply()
	if reply.Text != "fast reply" {
		t.Fatalf("reply text = %q, want fast reply", reply.Text)
	}
	if reply.ReplyToMessageID != "msg-fast" {
		t.Fatalf("reply target = %q, want msg-fast", reply.ReplyToMessageID)
	}
	if reply.ChannelID != "chan-fast" {
		t.Fatalf("reply channel = %q, want chan-fast", reply.ChannelID)
	}

	<-readLoopDone
}

func TestCodexAdapterHandleItemCompletedTracksKnownAgentMessages(t *testing.T) {
	t.Parallel()

	adapter := &CodexAdapter{
		turns: map[string]codexMirrorTarget{
			"turn-1": {ChannelID: "chan-1", MessageID: "msg-1"},
		},
		turnTexts: make(map[string][]codexThreadItem),
	}

	payload, err := json.Marshal(codexItemCompletedNotification{
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Item: codexThreadItem{
			Type:  "agentMessage",
			ID:    "item-1",
			Text:  "tracked",
			Phase: "final_answer",
		},
	})
	if err != nil {
		t.Fatalf("Marshal(tracked item) error = %v", err)
	}
	adapter.handleItemCompleted(payload)

	if got := len(adapter.turnTexts["turn-1"]); got != 1 {
		t.Fatalf("tracked item count = %d, want 1", got)
	}
	if got := adapter.turnTexts["turn-1"][0].Phase; got != "final_answer" {
		t.Fatalf("tracked item phase = %q, want final_answer", got)
	}

	ignoredPayloads := []codexItemCompletedNotification{
		{
			ThreadID: "thread-1",
			TurnID:   "turn-missing",
			Item:     codexThreadItem{Type: "agentMessage", ID: "item-2", Text: "ignore"},
		},
		{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			Item:     codexThreadItem{Type: "reasoning", ID: "item-3", Text: "ignore"},
		},
		{
			ThreadID: "thread-1",
			Item:     codexThreadItem{Type: "agentMessage", ID: "item-4", Text: "ignore"},
		},
	}

	for index, notification := range ignoredPayloads {
		payload, err := json.Marshal(notification)
		if err != nil {
			t.Fatalf("Marshal(ignored item %d) error = %v", index, err)
		}
		adapter.handleItemCompleted(payload)
	}

	if got := len(adapter.turnTexts["turn-1"]); got != 1 {
		t.Fatalf("tracked item count after ignored inputs = %d, want 1", got)
	}
}

func TestCodexAdapterPromotePendingMirrorLocked(t *testing.T) {
	t.Parallel()

	target := codexMirrorTarget{ChannelID: "chan-1", MessageID: "msg-1"}
	resultPayload, err := json.Marshal(codexTurnStartResponse{
		Turn: codexTurn{ID: "turn-promoted"},
	})
	if err != nil {
		t.Fatalf("Marshal(result) error = %v", err)
	}

	adapter := &CodexAdapter{
		pendingMirrors: map[int64]codexMirrorTarget{1: target},
	}
	if err := adapter.promotePendingMirrorLocked(1, codexResponseMessage{ID: 1, Result: resultPayload}); err != nil {
		t.Fatalf("promotePendingMirrorLocked(success) error = %v", err)
	}
	if got := adapter.turns["turn-promoted"]; got != target {
		t.Fatalf("promoted target = %#v, want %#v", got, target)
	}
	if len(adapter.pendingMirrors) != 0 {
		t.Fatalf("pendingMirrors after success = %d, want 0", len(adapter.pendingMirrors))
	}

	errorAdapter := &CodexAdapter{
		pendingMirrors: map[int64]codexMirrorTarget{2: target},
	}
	if err := errorAdapter.promotePendingMirrorLocked(2, codexResponseMessage{
		ID:    2,
		Error: &codexRPCError{Message: "turn/start failed"},
	}); err != nil {
		t.Fatalf("promotePendingMirrorLocked(rpc error) error = %v, want nil", err)
	}
	if len(errorAdapter.turns) != 0 {
		t.Fatalf("turns after rpc error = %d, want 0", len(errorAdapter.turns))
	}

	invalidCases := []struct {
		name     string
		response codexResponseMessage
		wantErr  string
	}{
		{
			name:     "missing result",
			response: codexResponseMessage{ID: 3},
			wantErr:  "missing result",
		},
		{
			name:     "invalid result json",
			response: codexResponseMessage{ID: 4, Result: json.RawMessage("{")},
			wantErr:  "decode turn/start response",
		},
		{
			name: "missing turn id",
			response: codexResponseMessage{
				ID:     5,
				Result: json.RawMessage(`{"turn":{"id":""}}`),
			},
			wantErr: "missing turn id",
		},
	}

	for index, testCase := range invalidCases {
		adapter := &CodexAdapter{
			pendingMirrors: map[int64]codexMirrorTarget{int64(index + 3): target},
		}
		err := adapter.promotePendingMirrorLocked(int64(index+3), testCase.response)
		if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
			t.Fatalf("%s error = %v, want substring %q", testCase.name, err, testCase.wantErr)
		}
		if len(adapter.pendingMirrors) != 0 {
			t.Fatalf("%s pendingMirrors = %d, want 0", testCase.name, len(adapter.pendingMirrors))
		}
	}
}

package channelbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/buildinfo"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/gorilla/websocket"
	"golang.org/x/sync/singleflight"
)

// --- Constants ---

const (
	codexAdapterName = "codex"

	codexTransportUnix = "unix"
	codexTransportWS   = "ws"

	codexRecoverSingleflightKey = "recover"
)

// codexThreadOrigin tracks where the currently cached thread id came from so
// recovery can skip thread/resume on ids we know were created in-memory on
// this app-server (auto-create) and therefore can never be on disk.
type codexThreadOrigin string

const (
	// codexThreadOriginConfigured means the id was supplied by the operator
	// via --thread or config. Resume is valid because the id usually comes
	// from an interactive codex shell that shares $CODEX_HOME/rollouts.
	codexThreadOriginConfigured codexThreadOrigin = "configured"
	// codexThreadOriginCached means the id was loaded from the on-disk cache
	// on startup. Treat like configured for recovery purposes because the
	// cached id originated from a prior configured or auto-created thread.
	codexThreadOriginCached codexThreadOrigin = "cached"
	// codexThreadOriginAutoCreated means the id was minted by thread/start on
	// this app-server. A later thread-not-found on this id must not try
	// resume because no rollout ever existed for it.
	codexThreadOriginAutoCreated codexThreadOrigin = "auto_created"
)

// --- Types ---

// CodexAdapter delivers inbound Discord events to a running Codex app-server.
type CodexAdapter struct {
	transport        string
	socketPath       string
	websocketURL     string
	threadID         string
	mirrorResponses  bool
	autoCreateThread bool

	session     discordpkg.Session
	audit       *AuditWriter
	threadStore *CodexThreadStore

	dialContext func(context.Context, string, string) (net.Conn, error)
	wsDialer    *websocket.Dialer

	serviceCtx       context.Context
	serviceCancel    context.CancelFunc
	reconnectBackoff []time.Duration

	mu           sync.Mutex
	conn         codexConnection
	connecting   bool
	connectWait  chan struct{}
	connectErr   error
	pending      map[int64]chan codexResponseMessage
	turns        map[string]codexMirrorTarget
	turnTexts    map[string][]string
	mirrorWG     sync.WaitGroup
	nextID       int64
	closeCh      chan struct{}
	closeErr     error
	closeOnce    sync.Once
	threadOrigin codexThreadOrigin
	recoverGroup singleflight.Group
}

// codexRecoverResult is the value returned by the singleflight recovery
// group. It carries both the recovered thread id and the label describing
// which fallback path produced it so concurrent callers share the outcome.
type codexRecoverResult struct {
	threadID     string
	fallbackPath string
	origin       codexThreadOrigin
}

type codexConnection interface {
	ReadJSON(context.Context) ([]byte, error)
	WriteJSON(context.Context, []byte) error
	Close() error
}

type codexUnixConnection struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

type codexWebsocketConnection struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

type codexRequestMessage struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type codexResponseMessage struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *codexRPCError  `json:"error,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
}

type codexNotificationMessage struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

type codexErrorResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Error   codexRPCError `json:"error"`
}

type codexRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type codexClientInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

type codexInitializeParams struct {
	ClientInfo   codexClientInfo `json:"clientInfo"`
	Capabilities map[string]any  `json:"capabilities"`
}

type codexInitializedParams struct{}

type codexThreadListParams struct {
	Limit       int      `json:"limit"`
	SortKey     string   `json:"sortKey"`
	SourceKinds []string `json:"sourceKinds"`
}

type codexThreadListResponse struct {
	Data []codexThreadInfo `json:"data"`
}

type codexThreadInfo struct {
	ID        string            `json:"id"`
	UpdatedAt int64             `json:"updatedAt"`
	Status    codexStatusHolder `json:"status"`
}

type codexStatusHolder struct {
	Type string `json:"type"`
}

type codexTurnStartParams struct {
	ThreadID string           `json:"threadId"`
	Input    []codexUserInput `json:"input"`
}

type codexThreadStartParams struct{}

type codexThreadStartResponse struct {
	Thread codexThreadRef `json:"thread"`
}

type codexThreadResumeParams struct {
	ThreadID string `json:"threadId"`
}

type codexThreadResumeResponse struct {
	Thread codexThreadRef `json:"thread"`
}

type codexThreadRef struct {
	ID string `json:"id"`
}

type codexUserInput struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	TextElements []any  `json:"text_elements,omitempty"`
}

type codexTurnStartResponse struct {
	Turn codexTurn `json:"turn"`
}

type codexTurn struct {
	ID    string          `json:"id"`
	Items []codexTurnItem `json:"items,omitempty"`
}

type codexTurnItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type codexTurnCompletedNotification struct {
	ThreadID string    `json:"threadId"`
	Turn     codexTurn `json:"turn"`
}

// codexItemCompletedNotification carries a single completed thread item (for
// example an agent message chunk) for a turn. The codex app-server emits the
// reply text exclusively via this notification - `turn/completed` carries an
// empty `items` slice per v2 protocol contract.
type codexItemCompletedNotification struct {
	ThreadID string         `json:"threadId"`
	TurnID   string         `json:"turnId"`
	Item     codexThreadItem `json:"item"`
}

// codexThreadItem mirrors the v2 `ThreadItem` tagged union. Only the
// `agentMessage` variant carries reply text the bridge needs to mirror. Other
// variants (reasoning, commandExecution, etc.) are ignored by leaving their
// fields zero-valued.
type codexThreadItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Text string `json:"text,omitempty"`
}

type codexMirrorTarget struct {
	ChannelID string
	GuildID   string
	MessageID string
}

// --- Constructors ---

// NewCodexAdapter creates a Codex bridge adapter.
func NewCodexAdapter(cfg CodexConfig, hookEnv HookEnv) (*CodexAdapter, error) {
	if hookEnv.HomeDir == "" {
		return nil, fmt.Errorf("home dir is required")
	}
	serviceCtx, serviceCancel := context.WithCancel(context.Background())

	adapter := &CodexAdapter{
		transport:        cfg.Transport,
		socketPath:       cfg.SocketPath,
		websocketURL:     cfg.WebsocketURL,
		threadID:         cfg.ThreadID,
		mirrorResponses:  cfg.MirrorResponses,
		autoCreateThread: cfg.AutoCreateThread,
		session:          hookEnv.Session,
		audit:            NewAuditWriter(hookEnv.HomeDir),
		dialContext:      (&net.Dialer{}).DialContext,
		wsDialer:         websocket.DefaultDialer,
		serviceCtx:       serviceCtx,
		serviceCancel:    serviceCancel,
		reconnectBackoff: []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond, 1600 * time.Millisecond},
		pending:          make(map[int64]chan codexResponseMessage),
		turns:            make(map[string]codexMirrorTarget),
		turnTexts:        make(map[string][]string),
		closeCh:          make(chan struct{}),
	}
	if cfg.ThreadID != "" {
		adapter.threadOrigin = codexThreadOriginConfigured
	}
	adapter.threadStore = NewCodexThreadStore(hookEnv.HomeDir, adapter)

	switch cfg.Transport {
	case codexTransportUnix:
		if cfg.SocketPath == "" {
			return nil, fmt.Errorf("codex unix transport requires socket path")
		}
	case codexTransportWS:
		if cfg.WebsocketURL == "" {
			return nil, fmt.Errorf("codex websocket transport requires websocket url")
		}
	default:
		return nil, fmt.Errorf("unsupported codex transport %q", cfg.Transport)
	}

	return adapter, nil
}

// --- Adapter ---

// Name returns the adapter name.
func (a *CodexAdapter) Name() string {
	return codexAdapterName
}

// Deliver sends one event to the active Codex thread.
func (a *CodexAdapter) Deliver(ctx context.Context, event Event) error {
	if err := a.ensureConnected(ctx); err != nil {
		return err
	}

	threadID, _, origin, err := a.resolveThreadID(ctx)
	if err != nil {
		return err
	}
	originalThreadID := threadID
	fallbackPath := "none"

	formatted := formatCodexInput(event)
	started, startErr := a.startTurn(ctx, threadID, formatted)
	if startErr != nil && isThreadNotFoundError(startErr) {
		recovered, recoverErr := a.recoverThreadSingleflight(ctx, threadID, origin)
		if recoverErr != nil {
			a.auditDelivery(event, originalThreadID, "", "error", recovered.fallbackPath, recoverErr.Error())
			return recoverErr
		}
		fallbackPath = recovered.fallbackPath
		slog.Warn("codex thread not found, recovered via fallback",
			"original_thread_id", originalThreadID,
			"recovered_thread_id", recovered.threadID,
			"fallback_path", fallbackPath,
		)
		threadID = recovered.threadID
		origin = recovered.origin
		started, startErr = a.startTurn(ctx, threadID, formatted)
	}
	if startErr != nil {
		a.auditDelivery(event, originalThreadID, threadID, "error", fallbackPath, startErr.Error())
		return startErr
	}

	if err := a.threadStore.SaveThread(threadID, origin); err != nil {
		a.auditDelivery(event, originalThreadID, threadID, "error", fallbackPath, err.Error())
		return fmt.Errorf("save codex thread: %w", err)
	}

	if a.audit != nil {
		meta := map[string]any{
			"chat_id":            event.Message.ChannelID,
			"message_id":         event.Message.ID,
			"user":               event.Message.AuthorUsername,
			"user_id":            event.Message.AuthorID,
			"thread_id":          threadID,
			"original_thread_id": originalThreadID,
			"fallback_path":      fallbackPath,
		}
		if err := a.audit.Append(a.Name(), event.Source, formatted, meta); err != nil {
			return fmt.Errorf("append codex channel audit: %w", err)
		}
	}
	a.auditDelivery(event, originalThreadID, threadID, "success", fallbackPath, "")

	if a.mirrorResponses && a.session != nil && started.Turn.ID != "" {
		a.mu.Lock()
		a.turns[started.Turn.ID] = codexMirrorTarget{
			ChannelID: event.Message.ChannelID,
			GuildID:   event.Message.GuildID,
			MessageID: event.Message.ID,
		}
		a.mu.Unlock()
	}

	return nil
}

// Close closes the adapter transport. Pending mirror dispatches are cancelled
// via the service context and awaited so Close does not return while a
// goroutine still holds a Reply in flight.
func (a *CodexAdapter) Close() error {
	if a.serviceCancel != nil {
		a.serviceCancel()
	}
	a.closeOnce.Do(func() {
		if a.closeCh != nil {
			close(a.closeCh)
		}
	})

	a.mu.Lock()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()

	a.mirrorWG.Wait()

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// ListThreads lists recent Codex threads for discovery.
func (a *CodexAdapter) ListThreads(ctx context.Context) ([]codexThreadSummary, error) {
	if err := a.ensureConnected(ctx); err != nil {
		return nil, err
	}

	var response codexThreadListResponse
	if err := a.request(ctx, "thread/list", codexThreadListParams{
		Limit:       20,
		SortKey:     "updated_at",
		SourceKinds: []string{"appServer"},
	}, &response); err != nil {
		return nil, err
	}

	threads := make([]codexThreadSummary, 0, len(response.Data))
	for _, thread := range response.Data {
		threads = append(threads, codexThreadSummary{
			ID:        thread.ID,
			Status:    thread.Status.Type,
			UpdatedAt: thread.UpdatedAt,
		})
	}
	sort.SliceStable(threads, func(i int, j int) bool {
		return threads[i].UpdatedAt > threads[j].UpdatedAt
	})
	return threads, nil
}

// --- Connection Management ---

func (a *CodexAdapter) ensureConnected(ctx context.Context) error {
	if a.isShuttingDown() {
		return context.Canceled
	}

	for {
		a.mu.Lock()
		if a.conn != nil {
			a.mu.Unlock()
			return nil
		}
		closeErr := a.closeErr
		if a.connecting {
			waitCh := a.connectWait
			a.mu.Unlock()

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-a.closeCh:
				return context.Canceled
			case <-waitCh:
			}

			a.mu.Lock()
			conn := a.conn
			connectErr := a.connectErr
			a.mu.Unlock()
			if conn != nil {
				return nil
			}
			if connectErr != nil {
				return connectErr
			}
			continue
		}

		waitCh := make(chan struct{})
		a.connecting = true
		a.connectWait = waitCh
		a.connectErr = nil
		a.mu.Unlock()

		err := a.connectWithRecovery(ctx, closeErr)

		a.mu.Lock()
		a.connecting = false
		a.connectWait = nil
		a.connectErr = err
		close(waitCh)
		a.mu.Unlock()
		return err
	}
}

func (a *CodexAdapter) connectWithRecovery(ctx context.Context, closeErr error) error {
	if closeErr == nil {
		return a.connectAndInitialize(ctx)
	}

	var lastErr error
	for _, delay := range a.reconnectBackoff {
		if err := a.waitForReconnectDelay(ctx, delay); err != nil {
			return err
		}
		lastErr = a.connectAndInitialize(ctx)
		if lastErr == nil {
			return nil
		}
	}
	if lastErr == nil {
		lastErr = closeErr
	}

	a.mu.Lock()
	a.closeErr = lastErr
	a.mu.Unlock()
	return lastErr
}

func (a *CodexAdapter) connectAndInitialize(ctx context.Context) error {
	conn, err := a.connect(ctx)
	if err != nil {
		return err
	}
	if err := a.initializeConnection(ctx, conn); err != nil {
		_ = conn.Close()
		return err
	}
	if a.isShuttingDown() {
		_ = conn.Close()
		return context.Canceled
	}

	a.mu.Lock()
	if a.conn != nil {
		a.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	a.conn = conn
	a.closeErr = nil
	a.mu.Unlock()

	go a.readLoop(a.readLoopContext())
	return nil
}

func (a *CodexAdapter) initializeConnection(ctx context.Context, conn codexConnection) error {
	if err := a.requestConnection(ctx, conn, "initialize", codexInitializeParams{
		ClientInfo: codexClientInfo{
			Name:    "exo-discord",
			Title:   "exo-discord",
			Version: buildinfo.Version,
		},
		Capabilities: map[string]any{},
	}, nil); err != nil {
		return fmt.Errorf("initialize codex app-server: %w", err)
	}
	if err := a.notifyConnection(ctx, conn, "initialized", codexInitializedParams{}); err != nil {
		return fmt.Errorf("notify codex app-server initialized: %w", err)
	}
	return nil
}

func (a *CodexAdapter) requestConnection(ctx context.Context, conn codexConnection, method string, params any, result any) error {
	id := atomic.AddInt64(&a.nextID, 1)
	if err := a.writeMessage(ctx, conn, codexRequestMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		return err
	}

	for {
		data, err := conn.ReadJSON(ctx)
		if err != nil {
			return fmt.Errorf("read codex response %s: %w", method, err)
		}

		var response codexResponseMessage
		if err := json.Unmarshal(data, &response); err != nil {
			continue
		}
		if response.Method != "" || response.ID != id {
			continue
		}
		if response.Error != nil {
			return fmt.Errorf("%s failed: %s", method, response.Error.Message)
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (a *CodexAdapter) notifyConnection(ctx context.Context, conn codexConnection, method string, params any) error {
	return a.writeMessage(ctx, conn, codexRequestMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (a *CodexAdapter) connect(ctx context.Context) (codexConnection, error) {
	switch a.transport {
	case codexTransportUnix:
		conn, err := a.dialContext(ctx, "unix", a.socketPath)
		if err != nil {
			return nil, fmt.Errorf("connect codex unix socket %s: %w", a.socketPath, err)
		}
		return &codexUnixConnection{
			conn:   conn,
			reader: bufio.NewReader(conn),
		}, nil
	case codexTransportWS:
		conn, response, err := a.wsDialer.DialContext(ctx, a.websocketURL, http.Header{})
		if err != nil {
			if response != nil && response.Body != nil {
				_ = response.Body.Close()
			}
			return nil, fmt.Errorf("connect codex websocket %s: %w", a.websocketURL, err)
		}
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return &codexWebsocketConnection{conn: conn}, nil
	default:
		return nil, fmt.Errorf("unsupported codex transport %q", a.transport)
	}
}

func (a *CodexAdapter) waitForReconnectDelay(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		if a.isShuttingDown() {
			return context.Canceled
		}
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-a.closeCh:
		return context.Canceled
	case <-timer.C:
		return nil
	}
}

func (a *CodexAdapter) readLoopContext() context.Context {
	if a.serviceCtx != nil {
		return a.serviceCtx
	}
	return context.Background()
}

func (a *CodexAdapter) mirrorContext() context.Context {
	if a.serviceCtx != nil {
		return a.serviceCtx
	}
	return context.Background()
}

func (a *CodexAdapter) isShuttingDown() bool {
	select {
	case <-a.closeCh:
		return true
	default:
		return false
	}
}

func (a *CodexAdapter) readLoop(ctx context.Context) {
	for {
		a.mu.Lock()
		conn := a.conn
		a.mu.Unlock()
		if conn == nil {
			return
		}

		data, err := conn.ReadJSON(ctx)
		if err != nil {
			shuttingDown := a.isShuttingDown()
			a.mu.Lock()
			if a.conn == conn {
				a.conn = nil
			}
			if !shuttingDown {
				a.closeErr = err
			}
			for id, pending := range a.pending {
				delete(a.pending, id)
				pending <- codexResponseMessage{Error: &codexRPCError{Message: err.Error()}}
			}
			a.mu.Unlock()
			_ = conn.Close()
			return
		}

		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(data, &envelope); err != nil {
			continue
		}

		if idRaw, ok := envelope["id"]; ok {
			if methodRaw, hasMethod := envelope["method"]; hasMethod {
				var method string
				if err := json.Unmarshal(methodRaw, &method); err == nil {
					a.handleServerRequest(ctx, method, idRaw)
				}
				continue
			}

			var id int64
			if err := json.Unmarshal(idRaw, &id); err != nil {
				continue
			}
			response := codexResponseMessage{ID: id}
			_ = json.Unmarshal(data, &response)

			a.mu.Lock()
			pending := a.pending[id]
			delete(a.pending, id)
			a.mu.Unlock()
			if pending != nil {
				pending <- response
			}
			continue
		}

		if methodRaw, ok := envelope["method"]; ok {
			var notification codexNotificationMessage
			if err := json.Unmarshal(data, &notification); err == nil {
				a.handleNotification(notification)
			} else {
				var method string
				if err := json.Unmarshal(methodRaw, &method); err == nil {
					a.handleNotification(codexNotificationMessage{Method: method})
				}
			}
		}
	}
}

func (a *CodexAdapter) handleServerRequest(ctx context.Context, method string, idRaw json.RawMessage) {
	var id int64
	if err := json.Unmarshal(idRaw, &id); err != nil {
		return
	}

	response, err := json.Marshal(codexErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: codexRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("unsupported server request: %s", method),
		},
	})
	if err != nil {
		return
	}

	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return
	}
	_ = conn.WriteJSON(ctx, response)
}

func (a *CodexAdapter) handleNotification(notification codexNotificationMessage) {
	if !a.mirrorResponses || a.session == nil {
		return
	}
	if a.isShuttingDown() {
		return
	}

	switch notification.Method {
	case "item/completed":
		a.handleItemCompleted(notification.Params)
	case "turn/completed":
		a.handleTurnCompleted(notification.Params)
	case "turn/failed":
		a.handleTurnFailed(notification.Params)
	}
}

// handleItemCompleted accumulates agentMessage text for the owning turn. The
// codex app-server emits the reply text via `item/completed` notifications,
// one per agent message (see codex-rs v2 protocol: ItemCompletedNotification
// with ThreadItem::AgentMessage). Non-agent items are ignored.
func (a *CodexAdapter) handleItemCompleted(params json.RawMessage) {
	var completed codexItemCompletedNotification
	if err := json.Unmarshal(params, &completed); err != nil {
		return
	}
	if completed.Item.Type != "agentMessage" || completed.Item.Text == "" {
		return
	}
	if completed.TurnID == "" {
		return
	}

	a.mu.Lock()
	if _, tracked := a.turns[completed.TurnID]; !tracked {
		a.mu.Unlock()
		return
	}
	a.turnTexts[completed.TurnID] = append(a.turnTexts[completed.TurnID], completed.Item.Text)
	a.mu.Unlock()
}

// handleTurnCompleted flushes accumulated agent message text to discord when
// the turn closes. The codex app-server always emits `turn/completed` with an
// empty items slice (codex-rs/app-server/src/bespoke_event_handling.rs line
// 1932: `items: vec![]`), so the reply text is reconstructed from the
// per-turn buffer written by `item/completed` handlers.
//
// Legacy fallback: if no text was buffered but the notification itself
// carries items (e.g. a future protocol change or a test harness), fall back
// to the last agent message inside the payload.
func (a *CodexAdapter) handleTurnCompleted(params json.RawMessage) {
	var completed codexTurnCompletedNotification
	if err := json.Unmarshal(params, &completed); err != nil {
		return
	}
	turnID := completed.Turn.ID
	if turnID == "" {
		return
	}

	a.mu.Lock()
	target, ok := a.turns[turnID]
	delete(a.turns, turnID)
	buffered := a.turnTexts[turnID]
	delete(a.turnTexts, turnID)
	a.mu.Unlock()
	if !ok {
		return
	}

	text := strings.Join(buffered, "\n\n")
	if text == "" {
		text = lastCodexAgentMessage(completed.Turn.Items)
	}
	if text == "" {
		return
	}
	if a.isShuttingDown() {
		return
	}

	a.dispatchMirror(target, turnID, completed.ThreadID, text)
}

// handleTurnFailed clears accumulated state for a failed turn so memory is
// bounded even when the turn never emits a successful completion.
func (a *CodexAdapter) handleTurnFailed(params json.RawMessage) {
	var failed struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(params, &failed); err != nil {
		return
	}
	a.mu.Lock()
	delete(a.turns, failed.Turn.ID)
	delete(a.turnTexts, failed.Turn.ID)
	a.mu.Unlock()
}

// dispatchMirror performs the outbound discord reply in a background
// goroutine so slow discord API calls never block the codex readLoop. The
// goroutine is tracked by `mirrorWG` so Close can wait for in-flight mirror
// deliveries to exit cleanly, and is bound to `serviceCtx` so shutdown
// cancels any pending Reply/SendMessage call.
func (a *CodexAdapter) dispatchMirror(target codexMirrorTarget, turnID string, threadID string, text string) {
	a.mirrorWG.Add(1)
	go func() {
		defer a.mirrorWG.Done()
		ctx := a.mirrorContext()
		if err := replyWithChunks(ctx, a.session, target, text); err != nil {
			slog.Warn("codex mirror reply failed",
				"adapter", a.Name(),
				"turn_id", turnID,
				"thread_id", threadID,
				"channel_id", target.ChannelID,
				"message_id", target.MessageID,
				"error", err,
			)
			return
		}
		slog.Info("codex mirror emitted",
			"adapter", a.Name(),
			"event", "mirror_emitted",
			"turn_id", turnID,
			"thread_id", threadID,
			"channel_id", target.ChannelID,
			"message_id", target.MessageID,
			"chunk_count", mirrorChunkCount(text),
			"char_count", len([]rune(text)),
		)
	}()
}

// mirrorChunkCount reports how many discord messages a mirrored reply will
// occupy after chunking. Exposed for audit logging only.
func mirrorChunkCount(text string) int {
	return len(splitDiscordChunks(text, 2000))
}

// --- JSON-RPC Helpers ---

func (a *CodexAdapter) request(ctx context.Context, method string, params any, result any) error {
	id := atomic.AddInt64(&a.nextID, 1)
	responseCh := make(chan codexResponseMessage, 1)

	a.mu.Lock()
	a.pending[id] = responseCh
	a.mu.Unlock()

	if err := a.write(ctx, codexRequestMessage{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}); err != nil {
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return err
	}

	select {
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.pending, id)
		a.mu.Unlock()
		return ctx.Err()
	case response := <-responseCh:
		if response.Error != nil {
			return fmt.Errorf("%s failed: %s", method, response.Error.Message)
		}
		if result == nil || len(response.Result) == 0 {
			return nil
		}
		if err := json.Unmarshal(response.Result, result); err != nil {
			return fmt.Errorf("decode %s response: %w", method, err)
		}
		return nil
	}
}

func (a *CodexAdapter) notify(ctx context.Context, method string, params any) error {
	return a.write(ctx, codexRequestMessage{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	})
}

func (a *CodexAdapter) write(ctx context.Context, message codexRequestMessage) error {
	a.mu.Lock()
	conn := a.conn
	a.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("codex connection is not available")
	}
	return a.writeMessage(ctx, conn, message)
}

func (a *CodexAdapter) writeMessage(ctx context.Context, conn codexConnection, message codexRequestMessage) error {
	line, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("marshal codex request %s: %w", message.Method, err)
	}
	if err := conn.WriteJSON(ctx, line); err != nil {
		return fmt.Errorf("write codex request %s: %w", message.Method, err)
	}
	return nil
}

// --- Thread Helpers ---

func (a *CodexAdapter) resolveThreadID(ctx context.Context) (string, bool, codexThreadOrigin, error) {
	a.mu.Lock()
	threadID := a.threadID
	origin := a.threadOrigin
	a.mu.Unlock()

	if threadID != "" {
		if origin == "" {
			origin = codexThreadOriginConfigured
		}
		return threadID, true, origin, nil
	}
	if saved, savedOrigin, ok := a.threadStore.LoadThread(); ok {
		if savedOrigin == "" {
			savedOrigin = codexThreadOriginCached
		}
		return saved, false, savedOrigin, nil
	}
	id, err := a.threadStore.DiscoverActiveThread(ctx)
	if err != nil {
		return "", false, "", err
	}
	return id, false, codexThreadOriginCached, nil
}

// recoverThreadSingleflight serializes concurrent recovery attempts for the
// same adapter so two simultaneous Deliver calls that both observe a
// thread-not-found on turn/start share a single recovery result and emit
// exactly one thread/start on the wire. Without this, each caller would race
// to create a fresh thread and orphan the losing ones on the app-server.
//
// On success the adapter state is updated inside the singleflight callback
// (threadID, threadOrigin, cache file) so the waiting callers see a coherent
// view.
func (a *CodexAdapter) recoverThreadSingleflight(ctx context.Context, requestedThreadID string, origin codexThreadOrigin) (codexRecoverResult, error) {
	value, err, _ := a.recoverGroup.Do(codexRecoverSingleflightKey, func() (any, error) {
		// re-check the in-memory thread id under the mutex: a prior winner
		// of this singleflight slot may already have recovered and advanced
		// a.threadID past the caller's stale requestedThreadID. if so, the
		// new id is the recovery result and no extra rpc is needed.
		a.mu.Lock()
		currentID := a.threadID
		currentOrigin := a.threadOrigin
		a.mu.Unlock()
		if currentID != "" && currentID != requestedThreadID {
			return codexRecoverResult{
				threadID:     currentID,
				fallbackPath: "cached",
				origin:       currentOrigin,
			}, nil
		}

		recoveredID, recoveredPath, recoveredOrigin, recoverErr := a.recoverThread(ctx, requestedThreadID, origin)
		if recoverErr != nil {
			return codexRecoverResult{fallbackPath: recoveredPath}, recoverErr
		}

		a.mu.Lock()
		a.threadID = recoveredID
		a.threadOrigin = recoveredOrigin
		a.mu.Unlock()

		if saveErr := a.threadStore.SaveThread(recoveredID, recoveredOrigin); saveErr != nil {
			return codexRecoverResult{fallbackPath: recoveredPath}, fmt.Errorf("save recovered codex thread: %w", saveErr)
		}

		return codexRecoverResult{
			threadID:     recoveredID,
			fallbackPath: recoveredPath,
			origin:       recoveredOrigin,
		}, nil
	})
	if err != nil {
		if result, ok := value.(codexRecoverResult); ok {
			return result, err
		}
		return codexRecoverResult{fallbackPath: "error"}, err
	}
	return value.(codexRecoverResult), nil
}

// recoverThread runs the fallback chain after a thread-not-found on
// turn/start: it attempts thread/resume for configured and cached ids,
// then thread/list discovery, then thread/start auto-create (if enabled),
// then returns the recovered id so the caller can retry turn/start with it.
// Returns the new thread id, a label identifying which fallback path
// succeeded, and the origin describing where the recovered id came from.
func (a *CodexAdapter) recoverThread(ctx context.Context, requestedThreadID string, origin codexThreadOrigin) (string, string, codexThreadOrigin, error) {
	// try thread/resume first for ids that could plausibly exist in
	// $CODEX_HOME/rollouts. ids minted by a prior thread/start on this
	// app-server (auto_created) never have a rollout, so resume would always
	// fail with thread not found and waste an rpc.
	if requestedThreadID != "" && origin != codexThreadOriginAutoCreated {
		resumedID, resumeErr := a.resumeThread(ctx, requestedThreadID)
		if resumeErr == nil && resumedID != "" {
			return resumedID, "resume", codexThreadOriginConfigured, nil
		}
		if resumeErr != nil {
			slog.Warn("codex thread resume failed, trying discovery",
				"thread_id", requestedThreadID,
				"error", resumeErr,
			)
		}
	}

	// fall back to discovery against the connected app-server
	discoveredThreadID, discoverErr := a.threadStore.DiscoverActiveThread(ctx)
	if discoverErr == nil && discoveredThreadID != "" {
		return discoveredThreadID, "discover", codexThreadOriginCached, nil
	}

	if !a.autoCreateThread {
		return "", "error", "", fmt.Errorf(
			"codex app-server has no thread matching %q and auto-create is disabled (pass --auto-create-thread, "+
				"start a conversation in the app-server first, or omit --thread to use discovery): %w",
			requestedThreadID, discoverErr,
		)
	}

	createdID, createErr := a.createThread(ctx)
	if createErr != nil {
		return "", "error", "", fmt.Errorf(
			"rediscover codex thread after thread not found: %w (auto-create also failed: %s)",
			discoverErr, createErr.Error(),
		)
	}
	return createdID, "auto_create", codexThreadOriginAutoCreated, nil
}

// resumeThread loads a thread by id from the app-server's rollout store.
func (a *CodexAdapter) resumeThread(ctx context.Context, threadID string) (string, error) {
	var response codexThreadResumeResponse
	if err := a.request(ctx, "thread/resume", codexThreadResumeParams{ThreadID: threadID}, &response); err != nil {
		return "", fmt.Errorf("resume codex thread %s: %w", threadID, err)
	}
	if response.Thread.ID == "" {
		return "", fmt.Errorf("resume codex thread %s: empty thread id in response", threadID)
	}
	return response.Thread.ID, nil
}

// createThread opens a brand new codex thread on the connected app-server.
func (a *CodexAdapter) createThread(ctx context.Context) (string, error) {
	var response codexThreadStartResponse
	if err := a.request(ctx, "thread/start", codexThreadStartParams{}, &response); err != nil {
		return "", fmt.Errorf("start codex thread: %w", err)
	}
	if response.Thread.ID == "" {
		return "", fmt.Errorf("start codex thread: empty thread id in response")
	}
	return response.Thread.ID, nil
}

// auditDelivery records the outcome of a codex delivery attempt via slog.
// The structured record captures the original requested thread id, the
// thread id the turn was actually started on, which fallback path ran,
// and whether the turn succeeded.
func (a *CodexAdapter) auditDelivery(event Event, originalThreadID string, finalThreadID string, result string, fallbackPath string, errMessage string) {
	attrs := []any{
		"adapter", a.Name(),
		"source", event.Source,
		"message_id", event.Message.ID,
		"channel_id", event.Message.ChannelID,
		"user_id", event.Message.AuthorID,
		"original_thread_id", originalThreadID,
		"final_thread_id", finalThreadID,
		"fallback_path", fallbackPath,
		"result", result,
	}
	if errMessage != "" {
		attrs = append(attrs, "error", errMessage)
		slog.Warn("codex delivery outcome", attrs...)
		return
	}
	slog.Info("codex delivery outcome", attrs...)
}

func (a *CodexAdapter) startTurn(ctx context.Context, threadID string, formatted string) (codexTurnStartResponse, error) {
	var response codexTurnStartResponse
	err := a.request(ctx, "turn/start", codexTurnStartParams{
		ThreadID: threadID,
		Input: []codexUserInput{
			{
				Type:         "text",
				Text:         formatted,
				TextElements: []any{},
			},
		},
	}, &response)
	if err != nil {
		return codexTurnStartResponse{}, err
	}
	return response, nil
}

func isThreadNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "thread not found")
}

// --- Formatting Helpers ---

func formatCodexInput(event Event) string {
	var buffer bytes.Buffer

	buffer.WriteString("Discord message received\n")
	buffer.WriteString("Author: ")
	buffer.WriteString(event.Message.AuthorUsername)
	if event.Message.AuthorID != "" {
		buffer.WriteString(" (")
		buffer.WriteString(event.Message.AuthorID)
		buffer.WriteString(")")
	}
	buffer.WriteString("\nChannel ID: ")
	buffer.WriteString(event.Message.ChannelID)

	if event.Message.GuildID != "" {
		buffer.WriteString("\nGuild ID: ")
		buffer.WriteString(event.Message.GuildID)
	}
	if event.Message.ThreadParentID != "" {
		buffer.WriteString("\nThread Parent ID: ")
		buffer.WriteString(event.Message.ThreadParentID)
	}

	buffer.WriteString("\nMessage ID: ")
	buffer.WriteString(event.Message.ID)
	buffer.WriteString("\nTimestamp: ")
	buffer.WriteString(event.Message.Timestamp.UTC().Format(time.RFC3339))

	if len(event.Message.Attachments) > 0 {
		buffer.WriteString("\nAttachments:")
		for _, attachment := range event.Message.Attachments {
			buffer.WriteString("\n- ")
			buffer.WriteString(attachment.Filename)
			buffer.WriteString(" (")
			buffer.WriteString(firstNonEmptyString(attachment.ContentType, "unknown"))
			buffer.WriteString(", ")
			buffer.WriteString(strconv.FormatInt(attachment.SizeBytes, 10))
			buffer.WriteString(" bytes)")
		}
	}

	buffer.WriteString("\n\nContent:\n")
	if event.Message.Content == "" {
		buffer.WriteString("(empty)")
	} else {
		buffer.WriteString(event.Message.Content)
	}

	return buffer.String()
}

func lastCodexAgentMessage(items []codexTurnItem) string {
	for index := len(items) - 1; index >= 0; index-- {
		if items[index].Type == "agentMessage" && items[index].Text != "" {
			return items[index].Text
		}
	}
	return ""
}

func replyWithChunks(ctx context.Context, session discordpkg.Session, target codexMirrorTarget, text string) error {
	chunks := splitDiscordChunks(text, 2000)
	if len(chunks) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	if _, err := session.Reply(ctx, discordpkg.ReplyRequest{
		ChannelID:        target.ChannelID,
		GuildID:          target.GuildID,
		Text:             chunks[0],
		ReplyToMessageID: target.MessageID,
	}); err != nil {
		return err
	}

	for _, chunk := range chunks[1:] {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := session.SendMessage(ctx, discordpkg.SendRequest{
			ChannelID: target.ChannelID,
			Text:      chunk,
		}); err != nil {
			return err
		}
	}
	return nil
}

func splitDiscordChunks(text string, limit int) []string {
	if text == "" {
		return nil
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return []string{text}
	}

	chunks := make([]string, 0, (len(runes)/limit)+1)
	for len(runes) > 0 {
		if len(runes) <= limit {
			chunks = append(chunks, string(runes))
			break
		}
		chunks = append(chunks, string(runes[:limit]))
		runes = runes[limit:]
	}
	return chunks
}

// --- Transport Helpers ---

func (c *codexUnixConnection) ReadJSON(_ context.Context) ([]byte, error) {
	line, err := c.reader.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return bytes.TrimSpace(line), nil
}

func (c *codexUnixConnection) WriteJSON(_ context.Context, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(append(payload, '\n'))
	return err
}

func (c *codexUnixConnection) Close() error {
	return c.conn.Close()
}

func (c *codexWebsocketConnection) ReadJSON(_ context.Context) ([]byte, error) {
	_, payload, err := c.conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (c *codexWebsocketConnection) WriteJSON(_ context.Context, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteMessage(websocket.TextMessage, payload)
}

func (c *codexWebsocketConnection) Close() error {
	return c.conn.Close()
}

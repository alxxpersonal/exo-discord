package channelbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/gorilla/websocket"
)

// --- Constants ---

const (
	codexAdapterName = "codex"

	codexTransportUnix = "unix"
	codexTransportWS   = "ws"
)

// --- Types ---

// CodexAdapter delivers inbound Discord events to a running Codex app-server.
type CodexAdapter struct {
	transport       string
	socketPath      string
	websocketURL    string
	threadID        string
	mirrorResponses bool

	session     discordpkg.Session
	audit       *AuditWriter
	threadStore *CodexThreadStore

	dialContext func(context.Context, string, string) (net.Conn, error)
	wsDialer    *websocket.Dialer

	mu       sync.Mutex
	conn     codexConnection
	pending  map[int64]chan codexResponseMessage
	turns    map[string]codexMirrorTarget
	nextID   int64
	closeCh  chan struct{}
	closeErr error
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

	adapter := &CodexAdapter{
		transport:       cfg.Transport,
		socketPath:      cfg.SocketPath,
		websocketURL:    cfg.WebsocketURL,
		threadID:        cfg.ThreadID,
		mirrorResponses: cfg.MirrorResponses,
		session:         hookEnv.Session,
		audit:           NewAuditWriter(hookEnv.HomeDir),
		dialContext:     (&net.Dialer{}).DialContext,
		wsDialer:        websocket.DefaultDialer,
		pending:         make(map[int64]chan codexResponseMessage),
		turns:           make(map[string]codexMirrorTarget),
		closeCh:         make(chan struct{}),
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

	threadID, explicit, err := a.resolveThreadID(ctx)
	if err != nil {
		return err
	}

	formatted := formatCodexInput(event)
	started, err := a.startTurn(ctx, threadID, formatted)
	if err != nil && !explicit && isThreadNotFoundError(err) {
		threadID, err = a.threadStore.DiscoverActiveThread(ctx)
		if err != nil {
			return fmt.Errorf("rediscover codex thread after thread not found: %w", err)
		}
		if saveErr := a.threadStore.SaveThread(threadID); saveErr != nil {
			return fmt.Errorf("save rediscovered codex thread: %w", saveErr)
		}
		started, err = a.startTurn(ctx, threadID, formatted)
	}
	if err != nil {
		return err
	}

	if err := a.threadStore.SaveThread(threadID); err != nil {
		return fmt.Errorf("save codex thread: %w", err)
	}

	if a.audit != nil {
		meta := map[string]any{
			"chat_id":    event.Message.ChannelID,
			"message_id": event.Message.ID,
			"user":       event.Message.AuthorUsername,
			"user_id":    event.Message.AuthorID,
			"thread_id":  threadID,
		}
		if err := a.audit.Append(a.Name(), event.Source, formatted, meta); err != nil {
			return fmt.Errorf("append codex channel audit: %w", err)
		}
	}

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

// Close closes the adapter transport.
func (a *CodexAdapter) Close() error {
	a.mu.Lock()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()

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
	a.mu.Lock()
	if a.conn != nil {
		a.mu.Unlock()
		return nil
	}
	a.mu.Unlock()

	conn, err := a.connect(ctx)
	if err != nil {
		return err
	}

	a.mu.Lock()
	if a.conn != nil {
		a.mu.Unlock()
		_ = conn.Close()
		return nil
	}
	a.conn = conn
	a.mu.Unlock()

	go a.readLoop(context.WithoutCancel(ctx))

	if err := a.request(ctx, "initialize", codexInitializeParams{
		ClientInfo: codexClientInfo{
			Name:    "exo-discord",
			Title:   "exo-discord",
			Version: "0.1.0",
		},
		Capabilities: map[string]any{},
	}, nil); err != nil {
		return fmt.Errorf("initialize codex app-server: %w", err)
	}
	if err := a.notify(ctx, "initialized", codexInitializedParams{}); err != nil {
		return fmt.Errorf("notify codex app-server initialized: %w", err)
	}
	return nil
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
			a.mu.Lock()
			if a.closeErr == nil {
				a.closeErr = err
			}
			for id, pending := range a.pending {
				delete(a.pending, id)
				pending <- codexResponseMessage{Error: &codexRPCError{Message: err.Error()}}
			}
			a.mu.Unlock()
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

	switch notification.Method {
	case "turn/completed":
		var completed codexTurnCompletedNotification
		if err := json.Unmarshal(notification.Params, &completed); err != nil {
			return
		}
		a.mu.Lock()
		target, ok := a.turns[completed.Turn.ID]
		delete(a.turns, completed.Turn.ID)
		a.mu.Unlock()
		if !ok {
			return
		}

		text := lastCodexAgentMessage(completed.Turn.Items)
		if text == "" {
			return
		}
		_ = replyWithChunks(context.Background(), a.session, target, text)
	case "turn/failed":
		var failed struct {
			Turn struct {
				ID string `json:"id"`
			} `json:"turn"`
		}
		if err := json.Unmarshal(notification.Params, &failed); err != nil {
			return
		}
		a.mu.Lock()
		delete(a.turns, failed.Turn.ID)
		a.mu.Unlock()
	}
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

func (a *CodexAdapter) resolveThreadID(ctx context.Context) (string, bool, error) {
	if a.threadID != "" {
		return a.threadID, true, nil
	}
	if saved, ok := a.threadStore.LoadThread(); ok {
		return saved, false, nil
	}
	id, err := a.threadStore.DiscoverActiveThread(ctx)
	if err != nil {
		return "", false, err
	}
	return id, false, nil
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

	if _, err := session.Reply(ctx, discordpkg.ReplyRequest{
		ChannelID:        target.ChannelID,
		GuildID:          target.GuildID,
		Text:             chunks[0],
		ReplyToMessageID: target.MessageID,
	}); err != nil {
		return err
	}

	for _, chunk := range chunks[1:] {
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

package channelbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- Test Cases ---

func TestCodexAdapterDeliverWritesTurnStartOverUnixSocket(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(os.TempDir(), fmt.Sprintf("exo-discord-%d.sock", time.Now().UnixNano()))
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Fatalf("Close() error = %v", closeErr)
		}
	}()
	defer func() {
		_ = os.Remove(socketPath)
	}()

	type capturedRequest struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	requests := make(chan capturedRequest, 3)

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer func() {
			_ = conn.Close()
		}()

		reader := bufio.NewReader(conn)
		for index := 0; index < 3; index++ {
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
			requests <- capturedRequest{
				Method: method,
				Params: request["params"].(map[string]any),
			}

			if id, ok := request["id"].(float64); ok {
				response := map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result": map[string]any{
						"turn": map[string]any{
							"id": "turn-1",
						},
					},
				}
				data, _ := json.Marshal(response)
				_, _ = conn.Write(append(data, '\n'))
			}
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
			Content:        "hello from discord",
			Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	initialize := <-requests
	if initialize.Method != "initialize" {
		t.Fatalf("first method = %q, want initialize", initialize.Method)
	}

	initialized := <-requests
	if initialized.Method != "initialized" {
		t.Fatalf("second method = %q, want initialized", initialized.Method)
	}

	turnStart := <-requests
	if turnStart.Method != "turn/start" {
		t.Fatalf("third method = %q, want turn/start", turnStart.Method)
	}
	if got := turnStart.Params["threadId"]; got != "thread-1" {
		t.Fatalf("threadId = %#v, want thread-1", got)
	}

	input := turnStart.Params["input"].([]any)
	item := input[0].(map[string]any)
	if item["type"] != "text" {
		t.Fatalf("input type = %#v, want text", item["type"])
	}
	if item["text"] == "" {
		t.Fatal("input text = empty, want formatted payload")
	}
}

func TestCodexAdapterDeliverWritesTurnStartOverWebsocket(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	type capturedRequest struct {
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	requests := make(chan capturedRequest, 3)

	server := http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer func() {
				_ = conn.Close()
			}()

			for index := 0; index < 3; index++ {
				_, payload, readErr := conn.ReadMessage()
				if readErr != nil {
					return
				}

				var request map[string]any
				if err := json.Unmarshal(payload, &request); err != nil {
					t.Errorf("Unmarshal() error = %v", err)
					return
				}
				requests <- capturedRequest{
					Method: request["method"].(string),
					Params: request["params"].(map[string]any),
				}

				if id, ok := request["id"].(float64); ok {
					response := map[string]any{
						"jsonrpc": "2.0",
						"id":      id,
						"result": map[string]any{
							"turn": map[string]any{
								"id": "turn-2",
							},
						},
					}
					data, _ := json.Marshal(response)
					_ = conn.WriteMessage(websocket.TextMessage, data)
				}
			}
		}),
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() {
		if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
			t.Fatalf("Close() error = %v", closeErr)
		}
	}()
	go func() {
		_ = server.Serve(listener)
	}()
	defer func() {
		_ = server.Close()
	}()

	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:    codexTransportWS,
		WebsocketURL: "ws://" + listener.Addr().String(),
		ThreadID:     "thread-2",
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
			Content:        "hello from websocket",
			Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	initialize := <-requests
	if initialize.Method != "initialize" {
		t.Fatalf("first method = %q, want initialize", initialize.Method)
	}

	initialized := <-requests
	if initialized.Method != "initialized" {
		t.Fatalf("second method = %q, want initialized", initialized.Method)
	}

	turnStart := <-requests
	if turnStart.Method != "turn/start" {
		t.Fatalf("third method = %q, want turn/start", turnStart.Method)
	}
	if got := turnStart.Params["threadId"]; got != "thread-2" {
		t.Fatalf("threadId = %#v, want thread-2", got)
	}
}

func TestCodexAdapterAutoCreatesThreadWhenAppServerEmpty(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(os.TempDir(), "exo-discord-autocreate-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
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
				writeUnixError(conn, request["id"], "thread not found")
			case 4:
				if method != "thread/start" {
					t.Errorf("step 4 method = %q, want thread/start", method)
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"thread": map[string]any{"id": "thread-fresh"},
				})
			case 5:
				if method != "turn/start" {
					t.Errorf("step 5 method = %q, want turn/start", method)
					return
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-fresh" {
					t.Errorf("autocreated threadId = %#v, want thread-fresh", params["threadId"])
					return
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"turn": map[string]any{"id": "turn-auto"},
				})
			}
		}
	}()

	homeDir := t.TempDir()
	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:        codexTransportUnix,
		SocketPath:       socketPath,
		ThreadID:         "thread-stale",
		AutoCreateThread: true,
	}, HookEnv{HomeDir: homeDir})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}
	// empty list provider forces the auto-create path
	adapter.threadStore = NewCodexThreadStore(homeDir, fakeThreadProvider{
		err: errors.New("no codex threads were returned by thread/list"),
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
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	if adapter.threadID != "thread-fresh" {
		t.Fatalf("adapter threadID = %q, want thread-fresh", adapter.threadID)
	}
	if saved, ok := adapter.threadStore.LoadThread(); !ok || saved != "thread-fresh" {
		t.Fatalf("cache thread = %q, %t, want thread-fresh, true", saved, ok)
	}

	<-done
}

func TestCodexAdapterFailsGracefullyWhenAutoCreateDisabled(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(os.TempDir(), "exo-discord-noautocreate-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
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
				writeUnixResponse(conn, request["id"], map[string]any{})
			case 1:
				// initialized notification, no reply
			case 2:
				if method != "turn/start" {
					t.Errorf("step 2 method = %q, want turn/start", method)
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 3:
				if method != "thread/resume" {
					t.Errorf("step 3 method = %q, want thread/resume", method)
				}
				writeUnixError(conn, request["id"], "thread not found")
			}
		}
	}()

	homeDir := t.TempDir()
	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:        codexTransportUnix,
		SocketPath:       socketPath,
		ThreadID:         "thread-stale",
		AutoCreateThread: false,
	}, HookEnv{HomeDir: homeDir})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}
	adapter.threadStore = NewCodexThreadStore(homeDir, fakeThreadProvider{
		err: errors.New("no codex threads were returned by thread/list"),
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
		t.Fatal("Deliver() error = nil, want auto-create disabled failure")
	}
	if !strings.Contains(err.Error(), "auto-create is disabled") {
		t.Fatalf("Deliver() error = %v, want auto-create disabled context", err)
	}
	if !strings.Contains(err.Error(), "--auto-create-thread") {
		t.Fatalf("Deliver() error = %v, want flag hint", err)
	}

	<-done
}

func TestCodexAdapterPersistsAutoCreatedThreadToCache(t *testing.T) {
	t.Parallel()

	socketPath := filepath.Join(os.TempDir(), "exo-discord-autocache-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".sock")
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
	// two inbound events: first auto-creates after resume+list fail, second must
	// reuse the cached id from ~/.exo-discord/codex-thread.json.
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
		for step := 0; step < 7; step++ {
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
				}
				writeUnixResponse(conn, request["id"], map[string]any{})
			case 1:
				if method != "initialized" {
					t.Errorf("step 1 method = %q, want initialized", method)
				}
			case 2:
				if method != "turn/start" {
					t.Errorf("step 2 method = %q, want turn/start", method)
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-stale" {
					t.Errorf("initial threadId = %#v, want thread-stale", params["threadId"])
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 3:
				if method != "thread/resume" {
					t.Errorf("step 3 method = %q, want thread/resume", method)
				}
				writeUnixError(conn, request["id"], "thread not found")
			case 4:
				if method != "thread/start" {
					t.Errorf("step 4 method = %q, want thread/start", method)
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"thread": map[string]any{"id": "thread-cached"},
				})
			case 5:
				if method != "turn/start" {
					t.Errorf("step 5 method = %q, want turn/start", method)
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-cached" {
					t.Errorf("auto-created threadId = %#v, want thread-cached", params["threadId"])
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"turn": map[string]any{"id": "turn-a"},
				})
			case 6:
				// second Deliver must reuse the cached thread id directly
				if method != "turn/start" {
					t.Errorf("step 6 method = %q, want turn/start", method)
				}
				params := request["params"].(map[string]any)
				if params["threadId"] != "thread-cached" {
					t.Errorf("reused threadId = %#v, want thread-cached", params["threadId"])
				}
				writeUnixResponse(conn, request["id"], map[string]any{
					"turn": map[string]any{"id": "turn-b"},
				})
			}
		}
	}()

	homeDir := t.TempDir()
	adapter, err := NewCodexAdapter(CodexConfig{
		Transport:        codexTransportUnix,
		SocketPath:       socketPath,
		ThreadID:         "thread-stale",
		AutoCreateThread: true,
	}, HookEnv{HomeDir: homeDir})
	if err != nil {
		t.Fatalf("NewCodexAdapter() error = %v", err)
	}
	adapter.threadStore = NewCodexThreadStore(homeDir, fakeThreadProvider{
		err: errors.New("no codex threads were returned by thread/list"),
	})

	event := Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "hello 1",
			Timestamp:      time.Now().UTC(),
		},
	}
	if err := adapter.Deliver(context.Background(), event); err != nil {
		t.Fatalf("Deliver(1) error = %v", err)
	}
	if saved, ok := adapter.threadStore.LoadThread(); !ok || saved != "thread-cached" {
		t.Fatalf("cache after first = %q, %t, want thread-cached, true", saved, ok)
	}
	if adapter.threadID != "thread-cached" {
		t.Fatalf("adapter threadID = %q, want thread-cached", adapter.threadID)
	}

	event.Message.ID = "msg-2"
	event.Message.Content = "hello 2"
	if err := adapter.Deliver(context.Background(), event); err != nil {
		t.Fatalf("Deliver(2) error = %v", err)
	}

	<-done
}

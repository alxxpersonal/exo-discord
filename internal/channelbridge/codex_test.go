package channelbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
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
	defer listener.Close()
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
		defer conn.Close()

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
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()

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
	defer listener.Close()
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

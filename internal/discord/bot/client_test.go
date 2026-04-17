package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/bwmarrin/discordgo"
	"github.com/gorilla/websocket"
)

// --- Test Doubles ---

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

// --- Test Cases ---

func TestNewDiscordGoClient(t *testing.T) {
	t.Parallel()

	client, err := NewDiscordGoClient("secret-token", discordgo.IntentGuildMessages)
	if err != nil {
		t.Fatalf("NewDiscordGoClient() error = %v", err)
	}
	if !client.session.StateEnabled {
		t.Fatal("StateEnabled = false, want true")
	}
	if got, want := client.session.Identify.Intents, discordgo.IntentGuildMessages; got != want {
		t.Fatalf("Identify.Intents = %v, want %v", got, want)
	}
}

func TestSendReplyEditReactAndFetchHistory(t *testing.T) {
	t.Parallel()

	var bodies []map[string]any
	session, err := discordgo.New("Bot secret")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.Client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.Body != nil {
				var body map[string]any
				if err := json.NewDecoder(req.Body).Decode(&body); err == nil {
					bodies = append(bodies, body)
				}
			}

			switch req.Method {
			case http.MethodPost:
				return jsonResponse(http.StatusOK, `{"id":"msg-send","channel_id":"chan-1","content":"hello"}`), nil
			case http.MethodPatch:
				return jsonResponse(http.StatusOK, `{"id":"msg-edit","channel_id":"chan-1","content":"edited"}`), nil
			case http.MethodPut:
				return jsonResponse(http.StatusNoContent, ``), nil
			case http.MethodGet:
				return jsonResponse(http.StatusOK, `[
					{"id":"2","channel_id":"chan-1","guild_id":"guild-1","content":"second","timestamp":"2026-04-16T22:01:00Z","author":{"id":"user-2","username":"other"}},
					{"id":"1","channel_id":"chan-1","guild_id":"guild-1","content":"first","timestamp":"2026-04-16T22:00:00Z","author":{"id":"user-1","username":"testuser"}}
				]`), nil
			default:
				return jsonResponse(http.StatusInternalServerError, `{"message":"unexpected method"}`), nil
			}
		}),
	}

	client := &DiscordGoClient{
		session:    session,
		httpClient: &http.Client{},
		botUserID:  "bot-1",
	}

	sent, err := client.SendMessage(context.Background(), discord.SendRequest{ChannelID: "chan-1", Text: "hello"})
	if err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if sent.MessageID != "msg-send" {
		t.Fatalf("SendMessage() message id = %q, want msg-send", sent.MessageID)
	}

	_, err = client.Reply(context.Background(), discord.ReplyRequest{
		ChannelID:        "chan-1",
		GuildID:          "guild-1",
		Text:             "reply",
		ReplyToMessageID: "msg-1",
	})
	if err != nil {
		t.Fatalf("Reply() error = %v", err)
	}

	if err := client.React(context.Background(), discord.ReactRequest{
		ChannelID: "chan-1",
		MessageID: "msg-1",
		Emoji:     "👍",
	}); err != nil {
		t.Fatalf("React() error = %v", err)
	}

	_, err = client.EditMessage(context.Background(), discord.EditRequest{
		ChannelID: "chan-1",
		MessageID: "msg-edit",
		Text:      "edited",
	})
	if err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}

	history, err := client.FetchHistory(context.Background(), discord.HistoryRequest{ChannelID: "chan-1", Limit: 2})
	if err != nil {
		t.Fatalf("FetchHistory() error = %v", err)
	}
	if got, want := len(history), 2; got != want {
		t.Fatalf("history length = %d, want %d", got, want)
	}
	if history[0].ID != "1" || history[1].ID != "2" {
		t.Fatalf("history order = %#v, want oldest to newest", history)
	}
	if bodies[1]["message_reference"] == nil {
		t.Fatalf("reply body = %#v, want message_reference", bodies[1])
	}
}

func TestDownloadAttachments(t *testing.T) {
	t.Parallel()

	session, err := discordgo.New("Bot secret")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.Client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{
				"id":"msg-1",
				"channel_id":"chan-1",
				"author":{"id":"user-1","username":"testuser"},
				"timestamp":"2026-04-16T22:00:00Z",
				"attachments":[{"id":"att-1","filename":"spec.png","content_type":"image/png","size":4,"url":"https://cdn.discord.test/att-1"}]
			}`), nil
		}),
	}

	downloadClient := &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("data")),
			}, nil
		}),
	}

	client := &DiscordGoClient{
		session:    session,
		httpClient: downloadClient,
	}

	dir := t.TempDir()
	files, err := client.DownloadAttachments(context.Background(), discord.DownloadRequest{
		ChannelID:      "chan-1",
		MessageID:      "msg-1",
		DestinationDir: dir,
		MaxBytes:       16,
	})
	if err != nil {
		t.Fatalf("DownloadAttachments() error = %v", err)
	}
	if got, want := len(files), 1; got != want {
		t.Fatalf("files length = %d, want %d", got, want)
	}
	if got, want := files[0].Filename, "att-1.png"; got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}
	data, err := os.ReadFile(filepath.Join(dir, "att-1.png"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got, want := string(data), "data"; got != want {
		t.Fatalf("downloaded body = %q, want %q", got, want)
	}
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	if got, want := buildDownloadedFilename("att-1", "folder/spec image.png"), "att-1.png"; got != want {
		t.Fatalf("buildDownloadedFilename() = %q, want %q", got, want)
	}

	path := filepath.Join(t.TempDir(), "spec.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	messageSend, cleanup, err := buildMessageSend("hello", []string{path}, nil)
	if err != nil {
		t.Fatalf("buildMessageSend() error = %v", err)
	}
	defer cleanup()
	if got, want := messageSend.Content, "hello"; got != want {
		t.Fatalf("Content = %q, want %q", got, want)
	}
	if got, want := len(messageSend.Files), 1; got != want {
		t.Fatalf("files length = %d, want %d", got, want)
	}

	session, err := discordgo.New("Bot secret")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}
	session.State = discordgo.NewState()
	if err := session.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatalf("GuildAdd() error = %v", err)
	}
	if err := session.State.ChannelAdd(&discordgo.Channel{
		ID:       "thread-1",
		GuildID:  "guild-1",
		Type:     discordgo.ChannelTypeGuildPublicThread,
		ParentID: "chan-1",
	}); err != nil {
		t.Fatalf("ChannelAdd() error = %v", err)
	}

	raw := mapRawMessage(session, &discordgo.MessageCreate{
		Message: &discordgo.Message{
			ID:        "msg-1",
			ChannelID: "thread-1",
			GuildID:   "guild-1",
			Author:    &discordgo.User{ID: "user-1", Username: "testuser"},
			Member:    &discordgo.Member{Roles: []string{"role-1"}},
			Content:   "hello",
			Mentions:  []*discordgo.User{{ID: "bot-1"}},
			ReferencedMessage: &discordgo.Message{
				Author: &discordgo.User{ID: "bot-1"},
			},
			Timestamp: time.Date(2026, 4, 16, 22, 0, 0, 0, time.UTC),
		},
	})
	if raw.ThreadParentID != "chan-1" {
		t.Fatalf("ThreadParentID = %q, want chan-1", raw.ThreadParentID)
	}
	if raw.ChannelKind != discord.ChannelKindPublicThread {
		t.Fatalf("ChannelKind = %q, want public_thread", raw.ChannelKind)
	}

	normalized := normalizeMessage("bot-1", &discordgo.Message{
		ID:        "msg-1",
		ChannelID: "chan-1",
		GuildID:   "guild-1",
		Author:    &discordgo.User{ID: "user-1", Username: "testuser"},
		Member:    &discordgo.Member{Roles: []string{"role-1"}},
		Content:   "hello",
		Mentions:  []*discordgo.User{{ID: "bot-1"}},
		ReferencedMessage: &discordgo.Message{
			Author: &discordgo.User{ID: "bot-1"},
		},
		Timestamp: time.Date(2026, 4, 16, 22, 0, 0, 0, time.UTC),
	})
	if !normalized.MentionedBot || !normalized.RepliedToBot {
		t.Fatalf("normalized message = %#v, want mention and reply flags", normalized)
	}
	if len(normalized.RoleIDs) != 1 || normalized.RoleIDs[0] != "role-1" {
		t.Fatalf("RoleIDs = %#v, want role-1", normalized.RoleIDs)
	}

	if _, err := mapActivityType("watching"); err != nil {
		t.Fatalf("mapActivityType() error = %v", err)
	}
	if _, err := mapActivityType("invalid"); err == nil {
		t.Fatal("mapActivityType() error = nil, want invalid activity error")
	}
	if got := mapChannelType(discordgo.ChannelTypeDM, ""); got != discord.ChannelKindDM {
		t.Fatalf("mapChannelType(dm) = %q, want dm", got)
	}
	if got := mapChannelType(discordgo.ChannelTypeGuildPrivateThread, "guild-1"); got != discord.ChannelKindPrivateThread {
		t.Fatalf("mapChannelType(private thread) = %q, want private_thread", got)
	}
}

func TestGatewayLifecycleAndMessageHandler(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade() error = %v", err)
			return
		}
		defer func() {
			_ = conn.Close()
		}()

		if err := conn.WriteJSON(map[string]any{
			"op": 10,
			"d": map[string]any{
				"heartbeat_interval": 60000,
			},
		}); err != nil {
			t.Errorf("WriteJSON(hello) error = %v", err)
			return
		}

		if _, _, err := conn.ReadMessage(); err != nil {
			t.Errorf("ReadMessage(identify) error = %v", err)
			return
		}

		if err := conn.WriteJSON(map[string]any{
			"op": 0,
			"t":  "READY",
			"s":  1,
			"d": map[string]any{
				"session_id": "sid-1",
				"user": map[string]any{
					"id":       "bot-1",
					"username": "bot",
				},
			},
		}); err != nil {
			t.Errorf("WriteJSON(ready) error = %v", err)
			return
		}

		if err := conn.WriteJSON(map[string]any{
			"op": 0,
			"t":  "MESSAGE_CREATE",
			"s":  2,
			"d": map[string]any{
				"id":         "msg-1",
				"channel_id": "thread-1",
				"guild_id":   "guild-1",
				"content":    "hello",
				"timestamp":  "2026-04-16T22:00:00Z",
				"author": map[string]any{
					"id":       "user-1",
					"username": "testuser",
				},
				"member": map[string]any{
					"roles": []string{"role-1"},
				},
				"mentions": []map[string]any{
					{"id": "bot-1"},
				},
				"referenced_message": map[string]any{
					"author": map[string]any{"id": "bot-1"},
				},
			},
		}); err != nil {
			t.Errorf("WriteJSON(message_create) error = %v", err)
			return
		}

		_, _, _ = conn.ReadMessage()
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, err := NewDiscordGoClient("secret", discordgo.IntentGuildMessages)
	if err != nil {
		t.Fatalf("NewDiscordGoClient() error = %v", err)
	}
	client.session.Client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, `{
				"url":"`+wsURL+`",
				"shards":1,
				"session_start_limit":{"total":1,"remaining":1,"reset_after":0,"max_concurrency":1}
			}`), nil
		}),
	}
	if err := client.session.State.GuildAdd(&discordgo.Guild{ID: "guild-1"}); err != nil {
		t.Fatalf("GuildAdd() error = %v", err)
	}
	if err := client.session.State.ChannelAdd(&discordgo.Channel{
		ID:       "thread-1",
		GuildID:  "guild-1",
		Type:     discordgo.ChannelTypeGuildPublicThread,
		ParentID: "chan-1",
	}); err != nil {
		t.Fatalf("ChannelAdd() error = %v", err)
	}

	received := make(chan discord.RawMessageEvent, 1)
	client.OnMessageCreate(func(_ context.Context, event discord.RawMessageEvent) {
		received <- event
	})

	if err := client.Open(); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && client.BotUserID() == "" {
		time.Sleep(10 * time.Millisecond)
	}

	if got, want := client.BotUserID(), "bot-1"; got != want {
		t.Fatalf("BotUserID() = %q, want %q", got, want)
	}

	if err := client.SetStatus(context.Background(), discord.StatusRequest{
		Presence:     "online",
		ActivityType: "watching",
		ActivityText: "tests",
	}); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}

	select {
	case event := <-received:
		if event.ThreadParentID != "chan-1" || len(event.RoleIDs) != 1 || event.RoleIDs[0] != "role-1" {
			t.Fatalf("event = %#v, want thread parent and role ids", event)
		}
		if event.ReferencedAuthorID != "bot-1" {
			t.Fatalf("ReferencedAuthorID = %q, want bot-1", event.ReferencedAuthorID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message event")
	}

	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// --- Helpers ---

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

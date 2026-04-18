package bot

import (
	"context"
	"sync"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Types ---

// Session adapts a bot client to the internal discord session interface.
type Session struct {
	client      Client
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]discord.InboundHandler
}

// --- Constructors ---

// NewSession creates a bot session from a client.
func NewSession(client Client) *Session {
	session := &Session{
		client:      client,
		subscribers: make(map[uint64]discord.InboundHandler),
	}
	client.OnMessageCreate(session.handleRawMessage)
	return session
}

// --- Lifecycle ---

// Open opens the underlying bot client.
func (s *Session) Open(context.Context) error {
	return s.client.Open()
}

// Close closes the underlying bot client.
func (s *Session) Close(context.Context) error {
	return s.client.Close()
}

// Mode returns the session mode.
func (s *Session) Mode() string {
	return "bot"
}

// --- Message Operations ---

// SendMessage sends a Discord message.
func (s *Session) SendMessage(ctx context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	return s.client.SendMessage(ctx, req)
}

// Reply sends a Discord reply.
func (s *Session) Reply(ctx context.Context, req discord.ReplyRequest) (discord.SentMessage, error) {
	return s.client.Reply(ctx, req)
}

// React adds an emoji reaction to a Discord message.
func (s *Session) React(ctx context.Context, req discord.ReactRequest) error {
	return s.client.React(ctx, req)
}

// EditMessage edits a Discord message.
func (s *Session) EditMessage(ctx context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	return s.client.EditMessage(ctx, req)
}

// FetchHistory returns normalized Discord channel history.
func (s *Session) FetchHistory(ctx context.Context, req discord.HistoryRequest) ([]discord.Message, error) {
	return s.client.FetchHistory(ctx, req)
}

// DownloadAttachments downloads Discord attachments to disk.
func (s *Session) DownloadAttachments(ctx context.Context, req discord.DownloadRequest) ([]discord.DownloadedFile, error) {
	return s.client.DownloadAttachments(ctx, req)
}

// SetStatus updates the Discord presence and activity.
func (s *Session) SetStatus(ctx context.Context, req discord.StatusRequest) error {
	return s.client.SetStatus(ctx, req)
}

// Subscribe registers an inbound message handler.
func (s *Session) Subscribe(handler discord.InboundHandler) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextID
	s.nextID++
	s.subscribers[id] = handler

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subscribers, id)
	}
}

// --- Message Helpers ---

func (s *Session) handleRawMessage(ctx context.Context, raw discord.RawMessageEvent) {
	// Always drop our own bot's echoes (to avoid reply loops) and webhook
	// messages (which have no real author to evaluate). Other bots fall
	// through to the access layer, which allowlists them via
	// allowed_user_ids the same way human accounts are filtered.
	if raw.AuthorID == s.client.BotUserID() || raw.WebhookID != "" {
		return
	}

	message := discord.Message{
		ID:             raw.ID,
		ChannelID:      raw.ChannelID,
		GuildID:        raw.GuildID,
		ThreadParentID: raw.ThreadParentID,
		ChannelKind:    raw.ChannelKind,
		AuthorID:       raw.AuthorID,
		AuthorUsername: raw.AuthorUsername,
		RoleIDs:        append([]string(nil), raw.RoleIDs...),
		Content:        raw.Content,
		MentionedBot:   contains(raw.MentionedUserIDs, s.client.BotUserID()),
		RepliedToBot:   raw.ReferencedAuthorID != "" && raw.ReferencedAuthorID == s.client.BotUserID(),
		Attachments:    append([]discord.Attachment(nil), raw.Attachments...),
		Timestamp:      raw.Timestamp,
	}

	s.mu.RLock()
	handlers := make([]discord.InboundHandler, 0, len(s.subscribers))
	for _, handler := range s.subscribers {
		handlers = append(handlers, handler)
	}
	s.mu.RUnlock()

	for _, handler := range handlers {
		go handler(ctx, message)
	}
}

// --- Slice Helpers ---

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

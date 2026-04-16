package discord

import (
	"context"
	"time"
)

// --- Message Types ---

// ChannelKind names the normalized Discord channel kind.
type ChannelKind string

const (
	ChannelKindUnknown       ChannelKind = "unknown"
	ChannelKindDM            ChannelKind = "dm"
	ChannelKindGuildText     ChannelKind = "guild_text"
	ChannelKindPublicThread  ChannelKind = "public_thread"
	ChannelKindPrivateThread ChannelKind = "private_thread"
)

// Attachment stores normalized attachment metadata.
type Attachment struct {
	ID          string
	Filename    string
	ContentType string
	SizeBytes   int64
	URL         string
}

// RawMessageEvent stores the raw inbound Discord message event.
type RawMessageEvent struct {
	ID                  string
	ChannelID           string
	GuildID             string
	ThreadParentID      string
	ChannelKind         ChannelKind
	AuthorID            string
	AuthorUsername      string
	RoleIDs             []string
	AuthorBot           bool
	WebhookID           string
	Content             string
	MentionedUserIDs    []string
	ReferencedMessageID string
	ReferencedAuthorID  string
	Attachments         []Attachment
	Timestamp           time.Time
}

// Message stores the normalized inbound Discord message.
type Message struct {
	ID             string
	ChannelID      string
	GuildID        string
	ThreadParentID string
	ChannelKind    ChannelKind
	AuthorID       string
	AuthorUsername string
	RoleIDs        []string
	Content        string
	MentionedBot   bool
	RepliedToBot   bool
	Attachments    []Attachment
	Timestamp      time.Time
}

// --- Outbound Request Types ---

// SendRequest stores a send-message request.
type SendRequest struct {
	ChannelID string
	Text      string
	Files     []string
}

// ReplyRequest stores a reply request.
type ReplyRequest struct {
	ChannelID        string
	GuildID          string
	Text             string
	ReplyToMessageID string
	Files            []string
}

// ReactRequest stores a reaction request.
type ReactRequest struct {
	ChannelID string
	MessageID string
	Emoji     string
}

// EditRequest stores an edit request.
type EditRequest struct {
	ChannelID string
	MessageID string
	Text      string
}

// HistoryRequest stores a history fetch request.
type HistoryRequest struct {
	ChannelID string
	BeforeID  string
	AfterID   string
	AroundID  string
	Limit     int
}

// DownloadRequest stores an attachment download request.
type DownloadRequest struct {
	ChannelID      string
	MessageID      string
	AttachmentID   string
	DestinationDir string
	MaxBytes       int64
}

// DownloadedFile stores a downloaded attachment.
type DownloadedFile struct {
	AttachmentID string
	Filename     string
	Path         string
	ContentType  string
	SizeBytes    int64
}

// StatusRequest stores a Discord status update request.
type StatusRequest struct {
	Presence     string
	ActivityType string
	ActivityText string
}

// SentMessage stores the result of a sent or edited message.
type SentMessage struct {
	ChannelID string
	MessageID string
	Content   string
}

// InboundHandler handles a normalized inbound message.
type InboundHandler func(context.Context, Message)

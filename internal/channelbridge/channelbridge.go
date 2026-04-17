package channelbridge

import (
	"context"
	"fmt"
	"io"
	"time"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Types ---

// Adapter delivers inbound Discord events to an external channel target.
type Adapter interface {
	Name() string
	Deliver(context.Context, Event) error
	Close() error
}

// Config stores adapter selection and adapter-specific settings.
type Config struct {
	Enabled []string
	Claude  ClaudeConfig
	Codex   CodexConfig
}

// ClaudeConfig stores Claude adapter settings.
type ClaudeConfig struct {
	PermissionRelay bool
}

// CodexConfig stores Codex adapter settings.
type CodexConfig struct {
	Transport       string
	SocketPath      string
	WebsocketURL    string
	ThreadID        string
	MirrorResponses bool
}

// HookEnv stores runtime dependencies shared by adapters.
type HookEnv struct {
	HomeDir      string
	ClaudeWriter io.Writer
	Session      discordpkg.Session
}

// Event carries the normalized Discord event delivered to a channel adapter.
type Event struct {
	ID         string       `json:"id"`
	Source     string       `json:"source"`
	Mode       string       `json:"mode"`
	ReceivedAt time.Time    `json:"received_at"`
	Message    MessageEvent `json:"message"`
	Access     AccessEvent  `json:"access"`
}

// MessageEvent stores message details for a channel event.
type MessageEvent struct {
	ID             string            `json:"id"`
	ChannelID      string            `json:"channel_id"`
	ChannelType    string            `json:"channel_type"`
	GuildID        string            `json:"guild_id,omitempty"`
	ThreadParentID string            `json:"thread_parent_id,omitempty"`
	AuthorID       string            `json:"author_id"`
	AuthorUsername string            `json:"author_username"`
	RoleIDs        []string          `json:"role_ids,omitempty"`
	Content        string            `json:"content"`
	MentionedBot   bool              `json:"mentioned_bot"`
	RepliedToBot   bool              `json:"replied_to_bot"`
	Attachments    []AttachmentEvent `json:"attachments,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
}

// AttachmentEvent stores attachment metadata for a channel event.
type AttachmentEvent struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
}

// AccessEvent stores access evaluation details for a channel event.
type AccessEvent struct {
	Reason             string `json:"reason"`
	EffectiveChannelID string `json:"effective_channel_id"`
}

// --- Constructors ---

// NewAdapter creates an adapter from channel bridge config.
func NewAdapter(cfg Config, hookEnv HookEnv) (Adapter, error) {
	if len(cfg.Enabled) == 0 {
		return nil, fmt.Errorf("channel adapter is not enabled")
	}

	switch cfg.Enabled[0] {
	case "claude":
		return nil, fmt.Errorf("claude adapter is not configured")
	case "codex":
		return nil, fmt.Errorf("codex adapter is not configured")
	default:
		return nil, fmt.Errorf("unsupported channel adapter %q", cfg.Enabled[0])
	}
}

package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Interfaces ---

// Client decides how the hook handles an inbound envelope.
type Client interface {
	Decide(context.Context, Envelope) (Response, error)
}

// --- Envelope Types ---

// Envelope carries the normalized Discord event sent to a hook.
type Envelope struct {
	ID         string          `json:"id"`
	Source     string          `json:"source"`
	Mode       string          `json:"mode"`
	ReceivedAt time.Time       `json:"received_at"`
	Message    MessageEnvelope `json:"message"`
	Access     AccessEnvelope  `json:"access"`
}

// MessageEnvelope stores message details for a hook envelope.
type MessageEnvelope struct {
	ID             string               `json:"id"`
	ChannelID      string               `json:"channel_id"`
	ChannelType    string               `json:"channel_type"`
	GuildID        string               `json:"guild_id,omitempty"`
	ThreadParentID string               `json:"thread_parent_id,omitempty"`
	AuthorID       string               `json:"author_id"`
	AuthorUsername string               `json:"author_username"`
	RoleIDs        []string             `json:"role_ids,omitempty"`
	Content        string               `json:"content"`
	MentionedBot   bool                 `json:"mentioned_bot"`
	RepliedToBot   bool                 `json:"replied_to_bot"`
	Attachments    []AttachmentEnvelope `json:"attachments,omitempty"`
	Timestamp      time.Time            `json:"timestamp"`
}

// AttachmentEnvelope stores attachment metadata for a hook envelope.
type AttachmentEnvelope struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
}

// AccessEnvelope stores access evaluation details for a hook envelope.
type AccessEnvelope struct {
	Reason             string `json:"reason"`
	EffectiveChannelID string `json:"effective_channel_id"`
}

// --- Decision Types ---

// Decision names the action a hook response requests.
type Decision string

const (
	DecisionSkip      Decision = "skip"
	DecisionDefer     Decision = "defer"
	DecisionReply     Decision = "reply"
	DecisionReact     Decision = "react"
	DecisionEdit      Decision = "edit"
	DecisionSetStatus Decision = "set_status"
)

// Response stores the parsed result returned by a hook.
type Response struct {
	Decision  Decision         `json:"decision"`
	Reply     *ReplyAction     `json:"reply,omitempty"`
	React     *ReactAction     `json:"react,omitempty"`
	Edit      *EditAction      `json:"edit,omitempty"`
	SetStatus *SetStatusAction `json:"set_status,omitempty"`
	Raw       json.RawMessage  `json:"-"`
}

// ReplyAction stores the reply payload returned by a hook.
type ReplyAction struct {
	Text             string   `json:"text"`
	ReplyToMessageID string   `json:"reply_to_message_id,omitempty"`
	Files            []string `json:"files,omitempty"`
}

// ReactAction stores the reaction payload returned by a hook.
type ReactAction struct {
	ChannelID string `json:"channel_id,omitempty"`
	MessageID string `json:"message_id,omitempty"`
	Emoji     string `json:"emoji"`
}

// EditAction stores the edit payload returned by a hook.
type EditAction struct {
	ChannelID string `json:"channel_id,omitempty"`
	MessageID string `json:"message_id"`
	Text      string `json:"text"`
}

// SetStatusAction stores the status payload returned by a hook.
type SetStatusAction struct {
	Presence     string `json:"presence"`
	ActivityType string `json:"activity_type,omitempty"`
	ActivityText string `json:"activity_text,omitempty"`
}

// --- Envelope Builders ---

// NewEnvelope creates a hook envelope from a Discord message.
func NewEnvelope(id string, mode string, message discord.Message, reason string, effectiveChannelID string, receivedAt time.Time) Envelope {
	attachments := make([]AttachmentEnvelope, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, AttachmentEnvelope{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			SizeBytes:   attachment.SizeBytes,
		})
	}

	return Envelope{
		ID:         id,
		Source:     "discord",
		Mode:       mode,
		ReceivedAt: receivedAt.UTC(),
		Message: MessageEnvelope{
			ID:             message.ID,
			ChannelID:      message.ChannelID,
			ChannelType:    string(message.ChannelKind),
			GuildID:        message.GuildID,
			ThreadParentID: message.ThreadParentID,
			AuthorID:       message.AuthorID,
			AuthorUsername: message.AuthorUsername,
			RoleIDs:        append([]string(nil), message.RoleIDs...),
			Content:        message.Content,
			MentionedBot:   message.MentionedBot,
			RepliedToBot:   message.RepliedToBot,
			Attachments:    attachments,
			Timestamp:      message.Timestamp.UTC(),
		},
		Access: AccessEnvelope{
			Reason:             reason,
			EffectiveChannelID: effectiveChannelID,
		},
	}
}

// --- Response Parsing ---

// DecodeResponse parses a single hook response document.
func DecodeResponse(data []byte) (Response, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return Response{}, fmt.Errorf("hook response body is empty")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var response Response
	if err := decoder.Decode(&response); err != nil {
		return Response{}, fmt.Errorf("failed to decode hook response: %w", err)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return Response{}, fmt.Errorf("hook response must contain a single JSON document")
	}

	if err := response.Validate(); err != nil {
		return Response{}, err
	}

	return response, nil
}

// Validate checks that a hook response matches its decision.
func (r Response) Validate() error {
	if r.Decision == "" {
		return fmt.Errorf("decision is required")
	}

	fields := 0
	if r.Reply != nil {
		fields++
	}
	if r.React != nil {
		fields++
	}
	if r.Edit != nil {
		fields++
	}
	if r.SetStatus != nil {
		fields++
	}

	switch r.Decision {
	case DecisionSkip, DecisionDefer:
		if fields != 0 {
			return fmt.Errorf("decision %q must not include action payloads", r.Decision)
		}
		return nil
	case DecisionReply:
		if fields != 1 || r.Reply == nil {
			return fmt.Errorf("decision %q requires only reply payload", r.Decision)
		}
		if r.Reply.Text == "" {
			return fmt.Errorf("reply text is required")
		}
		return nil
	case DecisionReact:
		if fields != 1 || r.React == nil {
			return fmt.Errorf("decision %q requires only react payload", r.Decision)
		}
		if r.React.Emoji == "" {
			return fmt.Errorf("react emoji is required")
		}
		return nil
	case DecisionEdit:
		if fields != 1 || r.Edit == nil {
			return fmt.Errorf("decision %q requires only edit payload", r.Decision)
		}
		if r.Edit.MessageID == "" {
			return fmt.Errorf("edit message_id is required")
		}
		if r.Edit.Text == "" {
			return fmt.Errorf("edit text is required")
		}
		return nil
	case DecisionSetStatus:
		if fields != 1 || r.SetStatus == nil {
			return fmt.Errorf("decision %q requires only set_status payload", r.Decision)
		}
		if r.SetStatus.Presence == "" {
			return fmt.Errorf("set_status presence is required")
		}
		return nil
	default:
		return fmt.Errorf("unknown decision %q", r.Decision)
	}
}

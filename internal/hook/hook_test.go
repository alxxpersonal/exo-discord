package hook

import (
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Cases ---

func TestNewEnvelope(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 16, 22, 0, 0, 0, time.UTC)
	envelope := NewEnvelope(
		"evt-1",
		"bot",
		discord.Message{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			GuildID:        "guild-1",
			ThreadParentID: "parent-1",
			ChannelKind:    discord.ChannelKindPublicThread,
			AuthorID:       "user-1",
			AuthorUsername: "testuser",
			RoleIDs:        []string{"role-1"},
			Content:        "hello",
			MentionedBot:   true,
			RepliedToBot:   false,
			Attachments: []discord.Attachment{
				{ID: "att-1", Filename: "spec.png", ContentType: "image/png", SizeBytes: 128},
			},
			Timestamp: now,
		},
		"allowlisted_channel",
		"parent-1",
		now,
	)

	if envelope.Source != "discord" {
		t.Fatalf("Source = %q, want discord", envelope.Source)
	}
	if envelope.Message.ChannelType != "public_thread" {
		t.Fatalf("ChannelType = %q, want public_thread", envelope.Message.ChannelType)
	}
	if envelope.Access.EffectiveChannelID != "parent-1" {
		t.Fatalf("EffectiveChannelID = %q, want parent-1", envelope.Access.EffectiveChannelID)
	}
	if len(envelope.Message.Attachments) != 1 || envelope.Message.Attachments[0].ID != "att-1" {
		t.Fatalf("attachments = %#v", envelope.Message.Attachments)
	}
}

func TestDecodeResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "skip",
			body: `{"decision":"skip"}`,
		},
		{
			name:    "unknown-decision",
			body:    `{"decision":"approve"}`,
			wantErr: `unknown decision "approve"`,
		},
		{
			name:    "payload-mismatch",
			body:    `{"decision":"skip","reply":{"text":"nope"}}`,
			wantErr: `decision "skip" must not include action payloads`,
		},
		{
			name:    "unknown-field",
			body:    `{"decision":"skip","extra":true}`,
			wantErr: "failed to decode hook response",
		},
		{
			name:    "missing-reply-text",
			body:    `{"decision":"reply","reply":{"reply_to_message_id":"msg-1"}}`,
			wantErr: "reply text is required",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response, err := DecodeResponse([]byte(test.body))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("DecodeResponse() error = %v", err)
				}
				if response.Decision == "" {
					t.Fatal("Decision = empty, want populated")
				}
				return
			}

			if err == nil || err.Error() == "" {
				t.Fatalf("DecodeResponse() error = %v, want %q", err, test.wantErr)
			}
			if got := err.Error(); got != test.wantErr && test.wantErr != "failed to decode hook response" {
				t.Fatalf("error = %q, want %q", got, test.wantErr)
			}
			if test.wantErr == "failed to decode hook response" && err != nil && gotPrefix(err.Error(), test.wantErr) == false {
				t.Fatalf("error = %q, want prefix %q", err.Error(), test.wantErr)
			}
		})
	}
}

// --- Helpers ---

func gotPrefix(value string, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

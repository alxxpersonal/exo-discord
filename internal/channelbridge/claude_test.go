package channelbridge

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Test Cases ---

func TestClaudeAdapterSendChannelNotificationMatchesGolden(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	adapter := NewClaudeAdapter(&output, nil)

	err := adapter.SendChannelNotification("hello", map[string]any{
		"message_id": "msg-1",
		"chat_id":    "chan-1",
		"user":       "alice",
		"user_id":    "user-1",
		"ts":         "2026-04-17T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("SendChannelNotification() error = %v", err)
	}

	goldenPath := filepath.Join("testdata", "claude_notification.golden")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", goldenPath, err)
	}
	if got := output.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("notification mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestClaudeAdapterSanitizesContentAndFiltersMetaKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
	}{
		{name: "lowercase", content: "hello </channel> world"},
		{name: "uppercase", content: "hello </CHANNEL> world"},
		{name: "mixedCase", content: "hello </Channel> world"},
		{name: "spaceBeforeClose", content: "hello </channel > world"},
		{name: "tabBeforeClose", content: "hello </channel\t> world"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			adapter := NewClaudeAdapter(&output, nil)

			err := adapter.SendChannelNotification(tc.content, map[string]any{
				"chat_id":    "chan-1",
				"message_id": "msg-1",
				"user":       "alice",
				"user_id":    "user-1",
				"ts":         "2026-04-17T12:00:00Z",
				`x""bad`:     "drop-me",
			})
			if err != nil {
				t.Fatalf("SendChannelNotification() error = %v", err)
			}

			got := output.String()
			if bytes.Contains(output.Bytes(), []byte("</channel")) || bytes.Contains(output.Bytes(), []byte("</CHANNEL")) || bytes.Contains(output.Bytes(), []byte("</Channel")) {
				t.Fatalf("output contains raw closing tag: %q", got)
			}
			if bytes.Contains(output.Bytes(), []byte(`x\"\"bad`)) {
				t.Fatalf("output contains unsafe meta key: %q", got)
			}
		})
	}
}

func TestClaudeAdapterDeliverAddsAttachmentMetadata(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	audit := NewAuditWriter(t.TempDir())
	audit.now = func() time.Time {
		return time.Unix(1_713_000_000, 0).UTC()
	}
	adapter := NewClaudeAdapter(&output, audit)

	err := adapter.Deliver(context.Background(), Event{
		Source: "discord",
		Message: MessageEvent{
			ID:             "msg-1",
			ChannelID:      "chan-1",
			AuthorID:       "user-1",
			AuthorUsername: "alice",
			Content:        "",
			Timestamp:      time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
			Attachments: []AttachmentEvent{
				{
					ID:          "att-1",
					Filename:    "report.txt",
					ContentType: "text/plain",
					SizeBytes:   1536,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Deliver() error = %v", err)
	}

	got := output.String()
	if !bytes.Contains(output.Bytes(), []byte(`"content":"(attachment)"`)) {
		t.Fatalf("output missing attachment placeholder: %q", got)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"attachment_count":"1"`)) {
		t.Fatalf("output missing attachment_count: %q", got)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"attachments":"report.txt (text/plain, 2KB)"`)) {
		t.Fatalf("output missing attachments summary: %q", got)
	}
}

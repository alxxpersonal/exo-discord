package channelbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// --- Constants ---

const (
	claudeAdapterName               = "claude"
	claudeChannelNotificationMethod = "notifications/claude/channel"
)

var safeClaudeMetaKeyRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
var claudeCloseTagRE = regexp.MustCompile(`(?i)</\s*channel\s*>`)

// --- Types ---

// ClaudeAdapter delivers inbound Discord messages to Claude Code channel notifications.
type ClaudeAdapter struct {
	writer io.Writer
	audit  *AuditWriter
}

type claudeNotification struct {
	JSONRPC string       `json:"jsonrpc"`
	Method  string       `json:"method"`
	Params  claudeParams `json:"params"`
}

type claudeParams struct {
	Content string            `json:"content"`
	Meta    orderedClaudeMeta `json:"meta"`
}

type orderedClaudeMeta map[string]string

// --- Constructors ---

// NewClaudeAdapter creates a Claude channel adapter.
func NewClaudeAdapter(writer io.Writer, audit *AuditWriter) *ClaudeAdapter {
	return &ClaudeAdapter{
		writer: writer,
		audit:  audit,
	}
}

// --- Adapter ---

// Name returns the adapter name.
func (a *ClaudeAdapter) Name() string {
	return claudeAdapterName
}

// Deliver sends one event to Claude Code as a channel notification.
func (a *ClaudeAdapter) Deliver(_ context.Context, event Event) error {
	content, meta := BuildClaudeNotificationPayload(event)
	return a.SendChannelNotification(content, meta)
}

// SendChannelNotification writes a Claude channel notification to the adapter writer.
func (a *ClaudeAdapter) SendChannelNotification(content string, meta map[string]any) error {
	if a.writer == nil {
		return fmt.Errorf("claude writer is required")
	}

	notification := claudeNotification{
		JSONRPC: "2.0",
		Method:  claudeChannelNotificationMethod,
		Params: claudeParams{
			Content: sanitizeClaudeContent(content),
			Meta:    normalizeClaudeMeta(meta),
		},
	}

	line, err := json.Marshal(notification)
	if err != nil {
		return fmt.Errorf("marshal claude channel notification: %w", err)
	}
	if _, err := a.writer.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write claude channel notification: %w", err)
	}

	if a.audit != nil {
		if err := a.audit.Append(a.Name(), "discord", notification.Params.Content, meta); err != nil {
			return fmt.Errorf("append claude channel audit: %w", err)
		}
	}

	return nil
}

// Close closes the adapter.
func (a *ClaudeAdapter) Close() error {
	return nil
}

// --- JSON Helpers ---

func (m orderedClaudeMeta) MarshalJSON() ([]byte, error) {
	const firstKey = "chat_id"

	if len(m) == 0 {
		return []byte("{}"), nil
	}

	order := []string{
		firstKey,
		"message_id",
		"user",
		"user_id",
		"ts",
		"attachment_count",
		"attachments",
	}

	var buffer bytes.Buffer
	buffer.WriteByte('{')

	first := true
	used := make(map[string]struct{}, len(m))
	for _, key := range order {
		value, ok := m[key]
		if !ok {
			continue
		}
		if err := writeClaudeMetaPair(&buffer, key, value, &first); err != nil {
			return nil, err
		}
		used[key] = struct{}{}
	}

	extraKeys := make([]string, 0, len(m)-len(used))
	for key := range m {
		if _, ok := used[key]; ok {
			continue
		}
		extraKeys = append(extraKeys, key)
	}
	sort.Strings(extraKeys)
	for _, key := range extraKeys {
		if err := writeClaudeMetaPair(&buffer, key, m[key], &first); err != nil {
			return nil, err
		}
	}

	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

func writeClaudeMetaPair(buffer *bytes.Buffer, key string, value string, first *bool) error {
	keyJSON, err := json.Marshal(key)
	if err != nil {
		return fmt.Errorf("marshal claude meta key %q: %w", key, err)
	}
	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal claude meta value %q: %w", key, err)
	}
	if !*first {
		buffer.WriteByte(',')
	}
	*first = false
	buffer.Write(keyJSON)
	buffer.WriteByte(':')
	buffer.Write(valueJSON)
	return nil
}

// --- Formatting Helpers ---

func normalizeClaudeMeta(meta map[string]any) orderedClaudeMeta {
	result := make(orderedClaudeMeta)
	for key, value := range meta {
		if !safeClaudeMetaKeyRE.MatchString(key) || value == nil {
			continue
		}
		result[key] = fmt.Sprint(value)
	}
	return result
}

func sanitizeClaudeContent(content string) string {
	return claudeCloseTagRE.ReplaceAllString(content, "<\\/channel>")
}

func claudeAttachmentSummary(attachments []AttachmentEvent) string {
	parts := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		parts = append(parts, fmt.Sprintf(
			"%s (%s, %sKB)",
			attachment.Filename,
			firstNonEmptyString(attachment.ContentType, "unknown"),
			strconv.Itoa(int(math.Round(float64(attachment.SizeBytes)/1024))),
		))
	}
	return strings.Join(parts, "; ")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// BuildClaudeNotificationPayload builds the Claude channel content and metadata for an event.
func BuildClaudeNotificationPayload(event Event) (string, map[string]any) {
	content := event.Message.Content
	if content == "" && len(event.Message.Attachments) > 0 {
		content = "(attachment)"
	}

	meta := map[string]any{
		"chat_id":    event.Message.ChannelID,
		"message_id": event.Message.ID,
		"user":       event.Message.AuthorUsername,
		"user_id":    event.Message.AuthorID,
		"ts":         event.Message.Timestamp.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if len(event.Message.Attachments) > 0 {
		meta["attachment_count"] = strconv.Itoa(len(event.Message.Attachments))
		meta["attachments"] = claudeAttachmentSummary(event.Message.Attachments)
	}

	return content, meta
}

package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/redact"
)

// --- Types ---

// Record records one audit event entry.
type Record struct {
	Timestamp  time.Time         `json:"ts"`
	Component  string            `json:"component,omitempty"`
	Mode       string            `json:"mode,omitempty"`
	Event      string            `json:"event"`
	GuildID    string            `json:"guild_id,omitempty"`
	ChannelID  string            `json:"channel_id,omitempty"`
	MessageID  string            `json:"message_id,omitempty"`
	UserID     string            `json:"user_id,omitempty"`
	Decision   string            `json:"decision,omitempty"`
	Source     string            `json:"source,omitempty"`
	DurationMS int64             `json:"duration_ms,omitempty"`
	Error      string            `json:"error,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"`
}

// Logger writes audit records to a JSONL file.
type Logger struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

// --- Constructors ---

// NewLogger creates an audit logger for a path.
func NewLogger(path string) *Logger {
	return &Logger{
		path: path,
		now:  time.Now,
	}
}

// --- Writes ---

// Write appends an audit record to the log.
func (l *Logger) Write(record Record) error {
	if record.Event == "" {
		return fmt.Errorf("audit event must not be empty")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if err := ensureAuditDir(filepath.Dir(l.path)); err != nil {
		return err
	}

	file, err := openAuditFile(l.path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record.withDefaults(l.now()).redacted()); err != nil {
		return fmt.Errorf("failed to encode audit record %s: %w", l.path, err)
	}

	return nil
}

// --- Filesystem Helpers ---

func ensureAuditDir(path string) error {
	_, err := os.Stat(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("failed to create audit dir %s: %w", path, err)
		}
		//nolint:gosec
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("failed to set audit dir permissions on %s: %w", path, err)
		}
	default:
		return fmt.Errorf("failed to stat audit dir %s: %w", path, err)
	}

	if err := config.RequireExactDirPerms(path, 0o700); err != nil {
		return err
	}
	return nil
}

func openAuditFile(path string) (*os.File, error) {
	file, err := openExistingAuditFile(path)
	switch {
	case err == nil:
		return file, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("failed to open audit log %s: %w", path, err)
	}

	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			file, err = openExistingAuditFile(path)
			if err != nil {
				return nil, fmt.Errorf("failed to open audit log %s after create race: %w", path, err)
			}
			return file, nil
		}
		return nil, fmt.Errorf("failed to create audit log %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to set audit log permissions on %s: %w", path, err)
	}
	if err := config.RequireExactFilePerms(path, 0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openExistingAuditFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return nil, err
	}
	if err := config.RequireExactFilePerms(path, 0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// --- Record Helpers ---

func (r Record) withDefaults(now time.Time) Record {
	if r.Timestamp.IsZero() {
		r.Timestamp = now.UTC()
		return r
	}

	r.Timestamp = r.Timestamp.UTC()
	return r
}

func (r Record) redacted() Record {
	r.Component = redact.String(r.Component)
	r.Mode = redact.String(r.Mode)
	r.Event = redact.String(r.Event)
	r.GuildID = redact.String(r.GuildID)
	r.ChannelID = redact.String(r.ChannelID)
	r.MessageID = redact.String(r.MessageID)
	r.UserID = redact.String(r.UserID)
	r.Decision = redact.String(r.Decision)
	r.Source = redact.String(r.Source)
	r.Error = redact.String(r.Error)

	if len(r.Fields) == 0 {
		return r
	}

	fields := make(map[string]string, len(r.Fields))
	for key, value := range r.Fields {
		fields[key] = redact.String(value)
	}
	r.Fields = fields
	return r
}

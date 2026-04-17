package channelbridge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
)

// --- Constants ---

const (
	channelStateDirName = ".exo-discord"
	channelAuditLogName = "channel-audit.log"
	channelAuditMaxSize = 10 * 1024 * 1024
)

// --- Types ---

// AuditWriter writes append-only channel bridge audit records.
type AuditWriter struct {
	mu   sync.Mutex
	path string
	now  func() time.Time
}

type auditRecord struct {
	Timestamp     time.Time `json:"ts"`
	Adapter       string    `json:"adapter"`
	Source        string    `json:"source"`
	ContentSHA256 string    `json:"content_sha256"`
	MetaKeys      []string  `json:"meta_keys,omitempty"`
}

// --- Constructors ---

// NewAuditWriter creates a channel bridge audit writer rooted in the home directory.
func NewAuditWriter(homeDir string) *AuditWriter {
	return &AuditWriter{
		path: filepath.Join(homeDir, channelStateDirName, channelAuditLogName),
		now:  time.Now,
	}
}

// Path returns the audit log path.
func (w *AuditWriter) Path() string {
	return w.path
}

// --- Writes ---

// Append writes a channel bridge audit record.
func (w *AuditWriter) Append(adapter string, source string, content string, meta map[string]any) error {
	if adapter == "" {
		return fmt.Errorf("channel audit adapter must not be empty")
	}
	if source == "" {
		return fmt.Errorf("channel audit source must not be empty")
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := ensureChannelAuditDir(filepath.Dir(w.path)); err != nil {
		return err
	}
	if err := rotateChannelAuditLog(w.path); err != nil {
		return err
	}

	file, err := openChannelAuditFile(w.path)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
	}()

	record := auditRecord{
		Timestamp:     w.now().UTC(),
		Adapter:       adapter,
		Source:        source,
		ContentSHA256: sha256Hex(content),
		MetaKeys:      sortedMetaKeys(meta),
	}

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(record); err != nil {
		return fmt.Errorf("encode channel audit record %s: %w", w.path, err)
	}

	return nil
}

// --- Filesystem Helpers ---

func ensureChannelAuditDir(path string) error {
	_, err := os.Stat(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create channel audit dir %s: %w", path, err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("chmod channel audit dir %s: %w", path, err)
		}
	default:
		return fmt.Errorf("stat channel audit dir %s: %w", path, err)
	}

	if err := config.RequireExactDirPerms(path, 0o700); err != nil {
		return err
	}
	return nil
}

func openChannelAuditFile(path string) (*os.File, error) {
	file, err := openExistingChannelAuditFile(path)
	switch {
	case err == nil:
		return file, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("open channel audit log %s: %w", path, err)
	}

	file, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			file, err = openExistingChannelAuditFile(path)
			if err != nil {
				return nil, fmt.Errorf("open channel audit log %s after create race: %w", path, err)
			}
			return file, nil
		}
		return nil, fmt.Errorf("create channel audit log %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("chmod channel audit log %s: %w", path, err)
	}
	if err := config.RequireExactFilePerms(path, 0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func openExistingChannelAuditFile(path string) (*os.File, error) {
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

func rotateChannelAuditLog(path string) error {
	info, err := os.Stat(path)
	switch {
	case err == nil:
	case errors.Is(err, os.ErrNotExist):
		return nil
	default:
		return fmt.Errorf("stat channel audit log %s: %w", path, err)
	}

	if info.Size() < channelAuditMaxSize {
		return nil
	}

	rotatedPath := path + ".1"
	if err := os.Remove(rotatedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove rotated channel audit log %s: %w", rotatedPath, err)
	}
	if err := os.Rename(path, rotatedPath); err != nil {
		return fmt.Errorf("rotate channel audit log %s to %s: %w", path, rotatedPath, err)
	}
	if err := config.RequireExactFilePerms(rotatedPath, 0o600); err != nil {
		return err
	}
	return nil
}

// --- Helpers ---

func sortedMetaKeys(meta map[string]any) []string {
	if len(meta) == 0 {
		return nil
	}

	keys := make([]string, 0, len(meta))
	for key := range meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sha256Hex(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

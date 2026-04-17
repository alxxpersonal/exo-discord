package channelbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
)

// --- Constants ---

const (
	codexThreadFileName    = "codex-thread.json"
	codexThreadTmpSuffix   = ".tmp"
	codexThreadFileVersion = 2
)

// --- Types ---

type threadListProvider interface {
	ListThreads(context.Context) ([]codexThreadSummary, error)
}

type codexThreadSummary struct {
	ID        string
	Status    string
	UpdatedAt int64
}

type codexThreadFile struct {
	Version   int       `json:"version,omitempty"`
	ThreadID  string    `json:"thread_id"`
	Origin    string    `json:"origin,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexThreadStore persists the most recent Codex thread id.
type CodexThreadStore struct {
	path     string
	provider threadListProvider
	now      func() time.Time
	writeMu  sync.Mutex
}

// --- Constructors ---

// NewCodexThreadStore creates a thread store rooted in the home directory.
func NewCodexThreadStore(homeDir string, provider threadListProvider) *CodexThreadStore {
	return &CodexThreadStore{
		path:     filepath.Join(homeDir, channelStateDirName, codexThreadFileName),
		provider: provider,
		now:      time.Now,
	}
}

// --- Discovery ---

// DiscoverActiveThread returns the most relevant active thread id.
func (s *CodexThreadStore) DiscoverActiveThread(ctx context.Context) (string, error) {
	if s.provider == nil {
		return "", fmt.Errorf("codex thread discovery provider is required")
	}

	threads, err := s.provider.ListThreads(ctx)
	if err != nil {
		return "", err
	}
	if len(threads) == 0 {
		return "", fmt.Errorf("no codex threads were returned by thread/list")
	}

	for _, thread := range threads {
		if thread.Status == "active" && thread.ID != "" {
			return thread.ID, nil
		}
	}

	for _, thread := range threads {
		if thread.ID != "" {
			return thread.ID, nil
		}
	}

	return "", fmt.Errorf("no codex thread id was returned by thread/list")
}

// SaveThread persists a thread id (and its origin) for later reuse. Writes
// are atomic: the payload is flushed to a sibling .tmp file and renamed
// into place so a process death or a concurrent SaveThread cannot truncate
// the cache file to an empty or partial JSON object. Matches the
// temp-then-rename pattern used by the channel audit writer.
func (s *CodexThreadStore) SaveThread(id string, origin codexThreadOrigin) error {
	if id == "" {
		return fmt.Errorf("codex thread id must not be empty")
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	if err := ensureChannelAuditDir(filepath.Dir(s.path)); err != nil {
		return err
	}

	data, err := json.Marshal(codexThreadFile{
		Version:   codexThreadFileVersion,
		ThreadID:  id,
		Origin:    string(origin),
		UpdatedAt: s.now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal codex thread file: %w", err)
	}

	tmpPath := s.path + codexThreadTmpSuffix
	// best-effort cleanup of a stale tmp from a prior crash; ignore missing.
	if err := os.Remove(tmpPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale codex thread tmp %s: %w", tmpPath, err)
	}
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write codex thread tmp %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod codex thread tmp %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename codex thread tmp to %s: %w", s.path, err)
	}
	return config.RequireExactFilePerms(s.path, 0o600)
}

// LoadThread returns the saved thread id and its origin if the cache file
// exists and parses. Older (v1) cache files without an origin field are
// treated as cached so recovery uses resume as the first fallback.
func (s *CodexThreadStore) LoadThread() (string, codexThreadOrigin, bool) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", "", false
	}

	var file codexThreadFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", "", false
	}
	if file.ThreadID == "" {
		return "", "", false
	}

	origin := codexThreadOrigin(file.Origin)
	switch origin {
	case codexThreadOriginConfigured, codexThreadOriginCached, codexThreadOriginAutoCreated:
	default:
		origin = codexThreadOriginCached
	}
	return file.ThreadID, origin, true
}

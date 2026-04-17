package channelbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/config"
)

// --- Constants ---

const codexThreadFileName = "codex-thread.json"

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
	ThreadID  string    `json:"thread_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CodexThreadStore persists the most recent Codex thread id.
type CodexThreadStore struct {
	path     string
	provider threadListProvider
	now      func() time.Time
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

// SaveThread persists a thread id for later reuse.
func (s *CodexThreadStore) SaveThread(id string) error {
	if id == "" {
		return fmt.Errorf("codex thread id must not be empty")
	}

	if err := ensureChannelAuditDir(filepath.Dir(s.path)); err != nil {
		return err
	}

	data, err := json.Marshal(codexThreadFile{
		ThreadID:  id,
		UpdatedAt: s.now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("marshal codex thread file: %w", err)
	}

	if err := os.WriteFile(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write codex thread file %s: %w", s.path, err)
	}
	if err := os.Chmod(s.path, 0o600); err != nil {
		return fmt.Errorf("chmod codex thread file %s: %w", s.path, err)
	}
	return config.RequireExactFilePerms(s.path, 0o600)
}

// LoadThread returns the saved thread id if it exists.
func (s *CodexThreadStore) LoadThread() (string, bool) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", false
	}

	var file codexThreadFile
	if err := json.Unmarshal(data, &file); err != nil {
		return "", false
	}
	if file.ThreadID == "" {
		return "", false
	}

	return file.ThreadID, true
}

// --- Helpers ---

func openCodexThreadFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		return nil, fmt.Errorf("read codex thread file %s: %w", path, err)
	}
	return data, nil
}

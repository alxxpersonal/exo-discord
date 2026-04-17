package channelbridge

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// --- Test Doubles ---

type fakeThreadListProvider struct {
	threads []codexThreadSummary
	err     error
}

func (f fakeThreadListProvider) ListThreads(context.Context) ([]codexThreadSummary, error) {
	if f.err != nil {
		return nil, f.err
	}
	return append([]codexThreadSummary(nil), f.threads...), nil
}

// --- Test Cases ---

func TestCodexThreadStoreSaveAndLoad(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{})
	if err := store.SaveThread("thread-1"); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	threadID, ok := store.LoadThread()
	if !ok {
		t.Fatal("LoadThread() ok = false, want true")
	}
	if threadID != "thread-1" {
		t.Fatalf("thread id = %q, want thread-1", threadID)
	}
}

func TestCodexThreadStoreDiscoverActiveThreadPrefersActive(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{
		threads: []codexThreadSummary{
			{ID: "thread-old", Status: "idle", UpdatedAt: 1},
			{ID: "thread-active", Status: "active", UpdatedAt: 2},
		},
	})

	threadID, err := store.DiscoverActiveThread(context.Background())
	if err != nil {
		t.Fatalf("DiscoverActiveThread() error = %v", err)
	}
	if threadID != "thread-active" {
		t.Fatalf("thread id = %q, want thread-active", threadID)
	}
}

func TestCodexThreadStoreDiscoverActiveThreadFallsBackToFirstThread(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{
		threads: []codexThreadSummary{
			{ID: "thread-idle", Status: "idle", UpdatedAt: 1},
		},
	})

	threadID, err := store.DiscoverActiveThread(context.Background())
	if err != nil {
		t.Fatalf("DiscoverActiveThread() error = %v", err)
	}
	if threadID != "thread-idle" {
		t.Fatalf("thread id = %q, want thread-idle", threadID)
	}
}

func TestCodexThreadStoreLoadThreadMissingFile(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{})
	threadID, ok := store.LoadThread()
	if ok {
		t.Fatalf("LoadThread() ok = true, want false with thread %q", threadID)
	}
}

func TestCodexThreadStoreSaveThreadRejectsEmptyID(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{})
	if err := store.SaveThread(""); err == nil {
		t.Fatal("SaveThread() error = nil, want error")
	}
}

func TestCodexThreadStoreLoadThreadRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	store := NewCodexThreadStore(t.TempDir(), fakeThreadListProvider{})
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(store.path, []byte("{bad"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if threadID, ok := store.LoadThread(); ok {
		t.Fatalf("LoadThread() = %q, true; want false", threadID)
	}
}

func TestCodexThreadStoreSaveThreadUsesStrictPerms(t *testing.T) {
	t.Parallel()

	homeDir := t.TempDir()
	store := NewCodexThreadStore(homeDir, fakeThreadListProvider{})
	if err := store.SaveThread("thread-1"); err != nil {
		t.Fatalf("SaveThread() error = %v", err)
	}

	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file perms = %04o, want 0600", info.Mode().Perm())
	}
}

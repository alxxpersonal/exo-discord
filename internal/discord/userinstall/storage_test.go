package userinstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Test Cases ---

func TestNewStorageCreatesDirWithSecurePerms(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "oauth")
	storage, err := NewStorage(dir)
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if storage.BaseDir() != dir {
		t.Fatalf("BaseDir() = %q, want %q", storage.BaseDir(), dir)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("dir perms = %04o, want %04o", got, want)
	}
}

func TestNewStorageRejectsEmpty(t *testing.T) {
	t.Parallel()
	if _, err := NewStorage(""); err == nil {
		t.Fatal("NewStorage(empty) error = nil, want error")
	}
}

func TestSaveLoadDeleteRoundTrip(t *testing.T) {
	t.Parallel()

	storage, err := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	stored := StoredToken{
		UserID:       "221773638772129792",
		Username:     "alxx",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "identify guilds",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
		ObtainedAt:   time.Now().UTC().Truncate(time.Second),
	}

	if err := storage.Save(stored); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	path, err := storage.PathFor(stored.UserID)
	if err != nil {
		t.Fatalf("PathFor() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("file perms = %04o, want %04o", got, want)
	}

	loaded, err := storage.Load(stored.UserID)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AccessToken != "access-1" || loaded.UserID != stored.UserID {
		t.Fatalf("Load() = %#v, want stored", loaded)
	}

	if err := storage.Delete(stored.UserID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := storage.Load(stored.UserID); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("Load() after delete error = %v, want ErrTokenNotFound", err)
	}
	if err := storage.Delete(stored.UserID); !errors.Is(err, ErrTokenNotFound) {
		t.Fatalf("Delete() second error = %v, want ErrTokenNotFound", err)
	}
}

func TestPathForRejectsInvalidUserIDs(t *testing.T) {
	t.Parallel()

	storage, err := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	for _, bad := range []string{"", "../escape", "abc", "1; rm -rf /", "12345abc"} {
		if _, err := storage.PathFor(bad); err == nil {
			t.Fatalf("PathFor(%q) error = nil, want rejection", bad)
		}
	}
}

func TestListReturnsSortedUserIDs(t *testing.T) {
	t.Parallel()

	storage, err := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}

	for _, id := range []string{"222", "111", "333"} {
		if err := storage.Save(StoredToken{UserID: id, AccessToken: "a"}); err != nil {
			t.Fatalf("Save(%q) error = %v", id, err)
		}
	}
	// ignore non-json + non-numeric files
	if err := os.WriteFile(filepath.Join(storage.BaseDir(), "ignore.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(ignore) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(storage.BaseDir(), "abc.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("WriteFile(abc) error = %v", err)
	}

	ids, err := storage.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []string{"111", "222", "333"}
	if len(ids) != len(want) {
		t.Fatalf("List() = %v, want %v", ids, want)
	}
	for idx, id := range ids {
		if id != want[idx] {
			t.Fatalf("List()[%d] = %q, want %q", idx, id, want[idx])
		}
	}
}

func TestListReturnsNilWhenDirMissing(t *testing.T) {
	t.Parallel()

	storage, err := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if err := os.RemoveAll(storage.BaseDir()); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}
	ids, err := storage.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if ids != nil {
		t.Fatalf("List() = %v, want nil", ids)
	}
}

package userinstall

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeRefresher struct {
	called int
	resp   TokenResponse
	err    error
}

func (f *fakeRefresher) RefreshToken(_ context.Context, _ string) (TokenResponse, error) {
	f.called++
	return f.resp, f.err
}

// --- Test Cases ---

func TestNewSessionLoadsTokenAndOpenVerifies(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	stored := StoredToken{
		UserID:       "221",
		Username:     "alxx",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		TokenType:    "Bearer",
		Scope:        "identify",
		ExpiresAt:    time.Now().Add(time.Hour),
		ObtainedAt:   time.Now(),
	}
	if err := storage.Save(stored); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"221","username":"alxx"}`))
	}))
	defer server.Close()

	session, err := NewSession(Config{
		UserID:     "221",
		Storage:    storage,
		Refresher:  &fakeRefresher{},
		HTTPClient: server.Client(),
		APIBase:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if session.Mode() != "user_install" {
		t.Fatalf("Mode() = %q, want user_install", session.Mode())
	}
	if err := session.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewSessionRejectsMissingDeps(t *testing.T) {
	t.Parallel()

	if _, err := NewSession(Config{}); err == nil {
		t.Fatal("NewSession(empty) error = nil, want missing storage")
	}
	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if _, err := NewSession(Config{Storage: storage}); err == nil {
		t.Fatal("NewSession(no refresher) error = nil")
	}
	if _, err := NewSession(Config{Storage: storage, Refresher: &fakeRefresher{}}); err == nil {
		t.Fatal("NewSession(no user id) error = nil")
	}
}

func TestSessionWriteOperationsReturnNotSupported(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err := storage.Save(StoredToken{
		UserID:       "221",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	session, err := NewSession(Config{
		UserID:    "221",
		Storage:   storage,
		Refresher: &fakeRefresher{},
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	ctx := context.Background()
	if _, err := session.SendMessage(ctx, discord.SendRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SendMessage() error = %v, want ErrNotSupported", err)
	}
	if _, err := session.Reply(ctx, discord.ReplyRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("Reply() error = %v, want ErrNotSupported", err)
	}
	if err := session.React(ctx, discord.ReactRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("React() error = %v, want ErrNotSupported", err)
	}
	if _, err := session.EditMessage(ctx, discord.EditRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("EditMessage() error = %v, want ErrNotSupported", err)
	}
	if _, err := session.FetchHistory(ctx, discord.HistoryRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("FetchHistory() error = %v, want ErrNotSupported", err)
	}
	if _, err := session.DownloadAttachments(ctx, discord.DownloadRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("DownloadAttachments() error = %v, want ErrNotSupported", err)
	}
	if err := session.SetStatus(ctx, discord.StatusRequest{}); !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetStatus() error = %v, want ErrNotSupported", err)
	}

	unsubscribe := session.Subscribe(func(context.Context, discord.Message) {})
	unsubscribe()
}

func TestSessionRefreshesExpiredToken(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	stored := StoredToken{
		UserID:       "221",
		Username:     "alxx",
		AccessToken:  "access-old",
		RefreshToken: "refresh-old",
		TokenType:    "Bearer",
		Scope:        "identify",
		ExpiresAt:    time.Now().Add(-time.Minute),
		ObtainedAt:   time.Now().Add(-time.Hour),
	}
	if err := storage.Save(stored); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	refresher := &fakeRefresher{
		resp: TokenResponse{
			AccessToken:  "access-new",
			RefreshToken: "refresh-new",
			TokenType:    "Bearer",
			Scope:        "identify",
			ExpiresIn:    3600,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-new" {
			t.Errorf("Authorization = %q, want bearer access-new", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"221","username":"alxx"}`))
	}))
	defer server.Close()

	session, err := NewSession(Config{
		UserID:     "221",
		Storage:    storage,
		Refresher:  refresher,
		HTTPClient: server.Client(),
		APIBase:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	if err := session.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if refresher.called != 1 {
		t.Fatalf("refresher called = %d, want 1", refresher.called)
	}

	persisted, err := storage.Load("221")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if persisted.AccessToken != "access-new" || persisted.RefreshToken != "refresh-new" {
		t.Fatalf("persisted = %#v, want rotated tokens", persisted)
	}
}

// --- M1: Concurrent Open / Subscribe race coverage ---

func TestSessionOpenIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err := storage.Save(StoredToken{
		UserID:       "221",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"221","username":"alxx"}`))
	}))
	defer server.Close()

	session, err := NewSession(Config{
		UserID:     "221",
		Storage:    storage,
		Refresher:  &fakeRefresher{},
		HTTPClient: server.Client(),
		APIBase:    server.URL,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			unsub := session.Subscribe(func(context.Context, discord.Message) {})
			unsub()
		}
		close(done)
	}()
	for i := 0; i < 50; i++ {
		if err := session.Open(context.Background()); err != nil {
			t.Fatalf("Open() error = %v", err)
		}
	}
	<-done
}

// --- M2: Reject mismatched user id between token and config ---

func TestNewSessionRejectsMismatchedUserID(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err := storage.Save(StoredToken{
		UserID:      "221",
		AccessToken: "access-1",
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	// rewrite the file so the payload user id differs from the filename key
	path, _ := storage.PathFor("221")
	tampered := `{"user_id":"999","access_token":"access-1","expires_at":"2099-01-01T00:00:00Z","obtained_at":"2099-01-01T00:00:00Z"}`
	if err := writeTamperedToken(path, tampered); err != nil {
		t.Fatalf("write tampered token: %v", err)
	}

	_, err := NewSession(Config{
		UserID:    "221",
		Storage:   storage,
		Refresher: &fakeRefresher{},
	})
	if err == nil {
		t.Fatal("NewSession() error = nil, want mismatch rejection")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want mismatch message", err)
	}
}

func writeTamperedToken(path string, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

func TestSessionRefreshErrorPropagates(t *testing.T) {
	t.Parallel()

	storage, _ := NewStorage(filepath.Join(t.TempDir(), "oauth"))
	if err := storage.Save(StoredToken{
		UserID:       "221",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	refresher := &fakeRefresher{err: errors.New("boom")}
	session, err := NewSession(Config{
		UserID:    "221",
		Storage:   storage,
		Refresher: refresher,
	})
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if err := session.Open(context.Background()); err == nil {
		t.Fatal("Open() error = nil, want refresh failure")
	}
}

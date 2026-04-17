package userinstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// --- Test Cases ---

func TestRESTClientCurrentUserAndGuilds(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer access-1" {
			t.Errorf("Authorization = %q, want bearer access-1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users/@me":
			_, _ = w.Write([]byte(`{"id":"221","username":"alxx","global_name":"Alxx"}`))
		case "/users/@me/guilds":
			_, _ = w.Write([]byte(`[{"id":"g1","name":"Test","owner":true}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewRESTClient(server.Client(), server.URL, "access-1")

	user, err := client.CurrentUser(context.Background())
	if err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}
	if user.ID != "221" || user.Username != "alxx" {
		t.Fatalf("user = %#v, want id=221", user)
	}

	guilds, err := client.CurrentGuilds(context.Background())
	if err != nil {
		t.Fatalf("CurrentGuilds() error = %v", err)
	}
	if len(guilds) != 1 || guilds[0].ID != "g1" {
		t.Fatalf("guilds = %#v, want g1", guilds)
	}
}

func TestRESTClientRequiresBearer(t *testing.T) {
	t.Parallel()

	client := NewRESTClient(nil, "http://invalid", "")
	if _, err := client.CurrentUser(context.Background()); err == nil {
		t.Fatal("CurrentUser() error = nil, want bearer required")
	}
}

func TestRESTClientPropagatesNon2xx(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"401: Unauthorized"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.Client(), server.URL, "bad-token")
	if _, err := client.CurrentUser(context.Background()); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("CurrentUser() error = %v, want unauthorized", err)
	}
}

func TestRESTClientSurfacesGenericErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.Client(), server.URL, "token")
	_, err := client.CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("CurrentUser() error = %v, want status 500", err)
	}
}

func TestRESTClientHonorsRateLimitHeaders(t *testing.T) {
	t.Parallel()

	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		if count == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset-After", "0.05")
		}
		_, _ = w.Write([]byte(`{"id":"221","username":"alxx"}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.Client(), server.URL, "token")
	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("CurrentUser() error = %v", err)
	}

	start := time.Now()
	if _, err := client.CurrentUser(context.Background()); err != nil {
		t.Fatalf("CurrentUser() second error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 30*time.Millisecond {
		t.Fatalf("rate limited call elapsed = %v, want >= 30ms wait", elapsed)
	}
}

func TestRESTClient429ReturnsRateLimitedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.01,"global":false}`))
	}))
	defer server.Close()

	client := NewRESTClient(server.Client(), server.URL, "token")
	_, err := client.CurrentUser(context.Background())
	if err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("CurrentUser() error = %v, want rate limited", err)
	}
}

// --- M5: waitForBucket respects context cancellation ---

func TestWaitForBucketReturnsContextErrorOnCancel(t *testing.T) {
	t.Parallel()

	client := NewRESTClient(nil, "http://x", "token")
	// seed a bucket reset far in the future
	client.mu.Lock()
	client.bucketReset["users-me"] = client.clock().Add(time.Hour)
	client.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := client.waitForBucket(ctx, "users-me")
	if err == nil {
		t.Fatal("waitForBucket() error = nil, want context error")
	}
	if !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("error = %v, want context canceled", err)
	}
	// bucket entry must be cleared on exit via defer
	client.mu.Lock()
	_, present := client.bucketReset["users-me"]
	client.mu.Unlock()
	if present {
		t.Fatal("bucketReset entry remained after wait")
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	if got := parseRetryAfter(""); got != 0 {
		t.Fatalf("parseRetryAfter(empty) = %v", got)
	}
	if got := parseRetryAfter("nope"); got != 0 {
		t.Fatalf("parseRetryAfter(invalid) = %v", got)
	}
	if got := parseRetryAfter("0.5"); got <= 0 {
		t.Fatalf("parseRetryAfter(0.5) = %v", got)
	}
}

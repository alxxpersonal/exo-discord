package hook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- Test Cases ---

func TestHTTPClientDecide(t *testing.T) {
	t.Parallel()

	t.Run("no-content-means-skip", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("method = %s, want POST", r.Method)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		client, err := NewHTTPClient(server.URL, time.Second, nil)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		response, err := client.Decide(context.Background(), Envelope{ID: "evt-1"})
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if response.Decision != DecisionSkip {
			t.Fatalf("Decision = %q, want skip", response.Decision)
		}
	})

	t.Run("parses-reply", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got, want := r.Header.Get("X-Test"), "ok"; got != want {
				t.Fatalf("X-Test = %q, want %q", got, want)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("ReadAll() error = %v", err)
			}
			if len(body) == 0 {
				t.Fatal("request body is empty")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"decision":"reply","reply":{"text":"hello"}}`))
		}))
		defer server.Close()

		client, err := NewHTTPClient(server.URL, time.Second, map[string]string{"X-Test": "ok"})
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		response, err := client.Decide(context.Background(), Envelope{ID: "evt-1"})
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if response.Reply == nil || response.Reply.Text != "hello" {
			t.Fatalf("Reply = %#v, want hello", response.Reply)
		}
	})

	t.Run("returns-status-errors", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusBadGateway)
		}))
		defer server.Close()

		client, err := NewHTTPClient(server.URL, time.Second, nil)
		if err != nil {
			t.Fatalf("NewHTTPClient() error = %v", err)
		}

		_, err = client.Decide(context.Background(), Envelope{ID: "evt-1"})
		if err == nil || err.Error() != "hook returned status 502" {
			t.Fatalf("Decide() error = %v, want hook returned status 502", err)
		}
	})
}

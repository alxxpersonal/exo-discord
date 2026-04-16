package hook

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// --- Test Cases ---

func TestStdioClientDecide(t *testing.T) {
	t.Parallel()

	t.Run("parses-stdout-and-forwards-stderr", func(t *testing.T) {
		t.Parallel()

		var stderr bytes.Buffer
		client, err := NewStdioClient(
			[]string{"sh", "-c", "cat >/dev/null; printf 'hook stderr' >&2; printf '{\"decision\":\"reply\",\"reply\":{\"text\":\"ready\"}}'"},
			&stderr,
		)
		if err != nil {
			t.Fatalf("NewStdioClient() error = %v", err)
		}

		response, err := client.Decide(context.Background(), Envelope{ID: "evt-1"})
		if err != nil {
			t.Fatalf("Decide() error = %v", err)
		}
		if response.Reply == nil || response.Reply.Text != "ready" {
			t.Fatalf("Reply = %#v, want ready", response.Reply)
		}
		if got := stderr.String(); got != "hook stderr" {
			t.Fatalf("stderr = %q, want hook stderr", got)
		}
	})

	t.Run("times-out", func(t *testing.T) {
		t.Parallel()

		client, err := NewStdioClient([]string{"sh", "-c", "sleep 1"}, nil)
		if err != nil {
			t.Fatalf("NewStdioClient() error = %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		_, err = client.Decide(ctx, Envelope{ID: "evt-1"})
		if err == nil || gotPrefix(err.Error(), "hook command timed out:") == false {
			t.Fatalf("Decide() error = %v, want timeout", err)
		}
	})
}

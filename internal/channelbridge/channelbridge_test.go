package channelbridge

import "testing"

// --- Test Cases ---

func TestNewAdapterRequiresEnabledAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewAdapter(Config{}, HookEnv{})
	if err == nil {
		t.Fatal("NewAdapter() error = nil, want error")
	}
}

func TestNewAdapterRejectsUnsupportedAdapter(t *testing.T) {
	t.Parallel()

	_, err := NewAdapter(Config{
		Enabled: []string{"unknown"},
	}, HookEnv{})
	if err == nil {
		t.Fatal("NewAdapter() error = nil, want error")
	}
}

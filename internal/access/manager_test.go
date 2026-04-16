package access

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Test Cases ---

func TestEvaluateGuildAllowAndDrop(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		AllowedChannelIDs: []string{"chan-1"},
		AllowedUserIDs:    []string{"user-1"},
		RequireMention:    true,
	})

	decision, err := manager.Evaluate(MessageContext{
		ChannelID:    "chan-1",
		UserID:       "user-1",
		MentionedBot: true,
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("Action = %q, want allow", decision.Action)
	}

	decision, err = manager.Evaluate(MessageContext{
		ChannelID: "chan-2",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionDrop || decision.Reason != "channel_not_allowlisted" {
		t.Fatalf("Decision = %#v, want channel_not_allowlisted", decision)
	}
}

func TestEvaluateDMCreatesAndResendsPairing(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		Pairing: PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Hour,
			MaxPending:  8,
			ResendLimit: 2,
		},
	})
	manager.newCode = func() (string, error) { return "AB12CD34", nil }

	first, err := manager.Evaluate(MessageContext{
		IsDM:      true,
		ChannelID: "dm-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if first.Action != ActionPair || first.PairingCode != "AB12CD34" || first.IsResend {
		t.Fatalf("first decision = %#v, want initial pairing", first)
	}

	second, err := manager.Evaluate(MessageContext{
		IsDM:      true,
		ChannelID: "dm-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if second.Action != ActionPair || second.PairingCode != "AB12CD34" || !second.IsResend {
		t.Fatalf("second decision = %#v, want resend pairing", second)
	}
}

func TestEvaluateDMSilencesAfterResendLimit(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		Pairing: PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Hour,
			MaxPending:  8,
			ResendLimit: 1,
		},
	})
	manager.newCode = func() (string, error) { return "AB12CD34", nil }

	if _, err := manager.Evaluate(MessageContext{IsDM: true, ChannelID: "dm-1", UserID: "user-1"}); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	decision, err := manager.Evaluate(MessageContext{IsDM: true, ChannelID: "dm-1", UserID: "user-1"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionDrop || decision.Reason != "pairing_pending_silenced" {
		t.Fatalf("Decision = %#v, want pairing_pending_silenced", decision)
	}
}

func TestApproveConsumesCode(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		Pairing: PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Hour,
			MaxPending:  8,
			ResendLimit: 2,
		},
	})
	manager.newCode = func() (string, error) { return "AB12CD34", nil }

	if _, err := manager.Evaluate(MessageContext{IsDM: true, ChannelID: "dm-1", UserID: "user-1"}); err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	pending, err := manager.Approve("AB12CD34")
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if pending.SenderID != "user-1" {
		t.Fatalf("approved sender = %q, want user-1", pending.SenderID)
	}

	if _, err := manager.Approve("AB12CD34"); err == nil {
		t.Fatal("Approve() error = nil, want invalid or expired")
	}

	decision, err := manager.Evaluate(MessageContext{IsDM: true, ChannelID: "dm-1", UserID: "user-1"})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionAllow || decision.Reason != "paired_user" {
		t.Fatalf("Decision = %#v, want paired_user allow", decision)
	}
}

func TestPendingPairsPrunesExpired(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		Pairing: PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Minute,
			MaxPending:  8,
			ResendLimit: 2,
		},
	})
	manager.state.PendingPairs["OLDPAIR"] = PendingPair{
		Code:      "OLDPAIR",
		SenderID:  "user-1",
		ChatID:    "dm-1",
		CreatedAt: time.Unix(0, 0),
		ExpiresAt: time.Unix(1, 0),
	}
	manager.now = func() time.Time { return time.Unix(10, 0) }

	decision, err := manager.Evaluate(MessageContext{
		ChannelID: "chan-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if len(manager.PendingPairs()) != 0 {
		t.Fatalf("PendingPairs() length = %d, want 0", len(manager.PendingPairs()))
	}
	if decision.Action != ActionDrop {
		t.Fatalf("Decision = %#v, want drop", decision)
	}
}

func TestSaveStateWritesPrivatePermissions(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state", "access.json")
	state := DefaultState()
	state.ApprovedUsers["user-1"] = ApprovedUser{ApprovedAt: time.Now().UTC(), Source: "pairing"}

	if err := SaveState(path, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("file perms = %04o, want %04o", got, want)
	}

	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("dir perms = %04o, want %04o", got, want)
	}
}

// --- Helpers ---

func newTestManager(t *testing.T, policy Policy) *Manager {
	t.Helper()

	statePath := filepath.Join(t.TempDir(), "access.json")
	manager, err := NewManager(statePath, policy)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.now = func() time.Time { return time.Unix(1_700_000_000, 0).UTC() }
	return manager
}

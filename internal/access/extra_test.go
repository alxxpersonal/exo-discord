package access

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
	"time"
)

// --- Test Cases ---

func TestEvaluateGuildRoleAllowlist(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{
		AllowedChannelIDs: []string{"chan-1"},
		AllowedRoleIDs:    []string{"role-2"},
	})

	decision, err := manager.Evaluate(MessageContext{
		ChannelID: "chan-1",
		UserID:    "user-1",
		RoleIDs:   []string{"role-1"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionDrop || decision.Reason != "role_not_allowlisted" {
		t.Fatalf("decision = %#v, want role_not_allowlisted", decision)
	}

	decision, err = manager.Evaluate(MessageContext{
		ChannelID: "chan-1",
		UserID:    "user-1",
		RoleIDs:   []string{"role-1", "role-2"},
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionAllow {
		t.Fatalf("decision = %#v, want allow", decision)
	}
}

func TestDenyRemovesPendingPair(t *testing.T) {
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
	if err := manager.Deny("AB12CD34"); err != nil {
		t.Fatalf("Deny() error = %v", err)
	}
	if len(manager.PendingPairs()) != 0 {
		t.Fatalf("PendingPairs() length = %d, want 0", len(manager.PendingPairs()))
	}
	if err := manager.Deny("AB12CD34"); err == nil {
		t.Fatal("Deny() error = nil, want invalid or expired")
	}
}

func TestGenerateCodeUppercaseHex(t *testing.T) {
	t.Parallel()

	code, err := GenerateCode()
	if err != nil {
		t.Fatalf("GenerateCode() error = %v", err)
	}
	if len(code) != 8 {
		t.Fatalf("code length = %d, want 8", len(code))
	}
	if ok, err := regexp.MatchString("^[A-F0-9]{8}$", code); err != nil || !ok {
		t.Fatalf("code = %q, want uppercase hex", code)
	}
}

func TestLoadStateRejectsBadFilePerms(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "access.json")
	//nolint:gosec
	if err := os.WriteFile(path, []byte(`{"approved_users":{},"pending_pairs":{}}`), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if _, err := LoadState(path); err == nil {
		t.Fatal("LoadState() error = nil, want permission error")
	}
}

func TestLoadStateMissingAndRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "access.json")
	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState(missing) error = %v", err)
	}
	if len(state.ApprovedUsers) != 0 || len(state.PendingPairs) != 0 {
		t.Fatalf("LoadState(missing) = %#v, want default state", state)
	}

	want := DefaultState()
	want.ApprovedUsers["user-1"] = ApprovedUser{ApprovedAt: time.Now().UTC(), Source: "pairing"}
	want.PendingPairs["AB12CD34"] = PendingPair{
		Code:      "AB12CD34",
		SenderID:  "user-1",
		ChatID:    "dm-1",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Nonce:     "AB12CD34:user-1",
	}
	if err := SaveState(path, want); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	got, err := LoadState(path)
	if err != nil {
		t.Fatalf("LoadState(round trip) error = %v", err)
	}
	if len(got.ApprovedUsers) != 1 || len(got.PendingPairs) != 1 {
		t.Fatalf("LoadState(round trip) = %#v", got)
	}
}

func TestPendingPairsReturnsSortedList(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{})
	manager.state.PendingPairs["B"] = PendingPair{
		Code:      "B",
		CreatedAt: time.Unix(20, 0).UTC(),
	}
	manager.state.PendingPairs["A"] = PendingPair{
		Code:      "A",
		CreatedAt: time.Unix(10, 0).UTC(),
	}

	pairs := manager.PendingPairs()
	if got, want := []string{pairs[0].Code, pairs[1].Code}, []string{"A", "B"}; !slices.Equal(got, want) {
		t.Fatalf("PendingPairs() order = %v, want %v", got, want)
	}
}

func TestHasAnyAndLoadStateInvalidJSON(t *testing.T) {
	t.Parallel()

	if !hasAny([]string{"a", "b"}, []string{"c", "b"}) {
		t.Fatal("hasAny() = false, want true")
	}
	if hasAny([]string{"a"}, []string{"b"}) {
		t.Fatal("hasAny() = true, want false")
	}

	path := filepath.Join(t.TempDir(), "access.json")
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := LoadState(path); err == nil {
		t.Fatal("LoadState() error = nil, want decode error")
	}
}

package access

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/audit"
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

func TestManagerHotReloadsAllowlistOnConfigChange(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.StartAutoReload(ctx, fixture.configPath, audit.NewLogger(fixture.auditPath)); err != nil {
		t.Fatalf("StartAutoReload() error = %v", err)
	}

	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))

	waitForManagerCondition(t, func() bool {
		manager.mu.RLock()
		defer manager.mu.RUnlock()
		return slices.Contains(manager.policy.AllowedUserIDs, "user-1")
	})

	decision, err := manager.Evaluate(MessageContext{
		IsDM:      true,
		ChannelID: "dm-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionAllow || decision.Reason != "allowlisted_user" {
		t.Fatalf("decision = %#v, want allowlisted_user", decision)
	}

	assertAuditReloadEvent(t, fixture.auditPath, "config")
}

func TestManagerHotReloadsAccessStateOnApproval(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.StartAutoReload(ctx, fixture.configPath, audit.NewLogger(fixture.auditPath)); err != nil {
		t.Fatalf("StartAutoReload() error = %v", err)
	}

	manager.newCode = func() (string, error) { return "AB12CD34", nil }
	decision, err := manager.Evaluate(MessageContext{
		IsDM:      true,
		ChannelID: "dm-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionPair {
		t.Fatalf("decision = %#v, want initial pairing", decision)
	}

	cliManager := newHotReloadManager(t, fixture, "")
	if _, err := cliManager.Approve("AB12CD34"); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}

	waitForManagerCondition(t, func() bool {
		manager.mu.RLock()
		defer manager.mu.RUnlock()
		_, ok := manager.state.ApprovedUsers["user-1"]
		return ok
	})

	decision, err = manager.Evaluate(MessageContext{
		IsDM:      true,
		ChannelID: "dm-1",
		UserID:    "user-1",
	})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if decision.Action != ActionAllow || decision.Reason != "paired_user" {
		t.Fatalf("decision = %#v, want paired_user allow", decision)
	}

	assertAuditReloadEvent(t, fixture.auditPath, "state")
}

func TestManagerHotReloadsConfigAndStateAsBoth(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.StartAutoReload(ctx, fixture.configPath, audit.NewLogger(fixture.auditPath)); err != nil {
		t.Fatalf("StartAutoReload() error = %v", err)
	}

	state := DefaultState()
	state.ApprovedUsers["user-2"] = ApprovedUser{
		ApprovedAt: time.Now().UTC(),
		Source:     "pairing",
	}
	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))
	if err := SaveState(fixture.statePath, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	waitForManagerCondition(t, func() bool {
		manager.mu.RLock()
		defer manager.mu.RUnlock()
		_, approved := manager.state.ApprovedUsers["user-2"]
		return approved && slices.Contains(manager.policy.AllowedUserIDs, "user-1")
	})

	assertAuditReloadEvent(t, fixture.auditPath, "both")
}

func TestManagerRespectsCtxCancelOnAutoReload(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")

	ctx, cancel := context.WithCancel(context.Background())
	if err := manager.StartAutoReload(ctx, fixture.configPath, nil); err != nil {
		t.Fatalf("StartAutoReload() error = %v", err)
	}
	cancel()

	time.Sleep(150 * time.Millisecond)
	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))

	time.Sleep(250 * time.Millisecond)
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if slices.Contains(manager.policy.AllowedUserIDs, "user-1") {
		t.Fatalf("AllowedUserIDs = %v, want watcher to stop after ctx cancel", manager.policy.AllowedUserIDs)
	}
}

func TestReloadPolicyReloadsConfigAndStateManually(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")
	manager.configPath = fixture.configPath

	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody("user-1"))
	state := DefaultState()
	state.ApprovedUsers["user-2"] = ApprovedUser{
		ApprovedAt: time.Now().UTC(),
		Source:     "pairing",
	}
	if err := SaveState(fixture.statePath, state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	if err := manager.ReloadPolicy(context.Background()); err != nil {
		t.Fatalf("ReloadPolicy() error = %v", err)
	}

	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if !slices.Contains(manager.policy.AllowedUserIDs, "user-1") {
		t.Fatalf("AllowedUserIDs = %v, want reloaded config user", manager.policy.AllowedUserIDs)
	}
	if _, ok := manager.state.ApprovedUsers["user-2"]; !ok {
		t.Fatalf("ApprovedUsers = %#v, want reloaded state approval", manager.state.ApprovedUsers)
	}
}

func TestReloadPolicyKeepsPreviousSnapshotOnError(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "user-1")
	manager.configPath = fixture.configPath

	writeHotReloadConfig(t, fixture.configPath, "{\n")

	if err := manager.ReloadPolicy(context.Background()); err == nil {
		t.Fatal("ReloadPolicy() error = nil, want config decode error")
	}

	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if !slices.Contains(manager.policy.AllowedUserIDs, "user-1") {
		t.Fatalf("AllowedUserIDs = %v, want previous in-memory snapshot preserved", manager.policy.AllowedUserIDs)
	}
}

func TestStartAutoReloadRejectsSecondStart(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := manager.StartAutoReload(ctx, fixture.configPath, nil); err != nil {
		t.Fatalf("StartAutoReload() first error = %v", err)
	}
	if err := manager.StartAutoReload(ctx, fixture.configPath, nil); err == nil {
		t.Fatal("StartAutoReload() second error = nil, want duplicate start error")
	}
}

func TestStartAutoReloadNoOpWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	fixture := newHotReloadFixture(t)
	manager := newHotReloadManager(t, fixture, "")
	var logBuffer bytes.Buffer
	manager.SetLogger(slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := manager.StartAutoReload(ctx, fixture.configPath, nil); err != nil {
		t.Fatalf("StartAutoReload() error = %v", err)
	}

	manager.mu.RLock()
	defer manager.mu.RUnlock()
	if manager.watcher != nil {
		t.Fatal("watcher = non-nil, want no watcher when context is already canceled")
	}
	if !strings.Contains(logBuffer.String(), "access auto-reload not started because context is already canceled") {
		t.Fatalf("debug log = %q, want canceled start message", logBuffer.String())
	}
}

func TestRecordReloadLockedWithoutAuditor(t *testing.T) {
	t.Parallel()

	manager := newTestManager(t, Policy{})
	manager.recordReloadLocked(ReloadSourceConfig)
}

// --- Helpers ---

type hotReloadFixture struct {
	configPath string
	statePath  string
	auditPath  string
}

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

func newHotReloadFixture(t *testing.T) hotReloadFixture {
	t.Helper()

	root := t.TempDir()
	workspaceDir := filepath.Join(root, "workspace")
	homeDir := filepath.Join(root, "home")
	stateDir := filepath.Join(homeDir, ".exo-discord")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(workspaceDir) error = %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatalf("MkdirAll(stateDir) error = %v", err)
	}

	fixture := hotReloadFixture{
		configPath: filepath.Join(workspaceDir, ".exo-discord"),
		statePath:  filepath.Join(stateDir, "access.json"),
		auditPath:  filepath.Join(stateDir, "audit.log"),
	}
	writeHotReloadConfig(t, fixture.configPath, hotReloadConfigBody(""))
	if err := SaveState(fixture.statePath, DefaultState()); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}
	return fixture
}

func newHotReloadManager(t *testing.T, fixture hotReloadFixture, allowedUser string) *Manager {
	t.Helper()

	manager, err := NewManager(fixture.statePath, Policy{
		AllowedUserIDs: nil,
		Pairing: PairingPolicy{
			Enabled:     true,
			CodeTTL:     time.Hour,
			MaxPending:  8,
			ResendLimit: 2,
		},
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	manager.now = time.Now
	if allowedUser != "" {
		manager.policy.AllowedUserIDs = []string{allowedUser}
	}
	return manager
}

func writeHotReloadConfig(t *testing.T, path string, body string) {
	t.Helper()

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", tempPath, err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		t.Fatalf("Chmod(%q) error = %v", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		t.Fatalf("Rename(%q, %q) error = %v", tempPath, path, err)
	}
}

func hotReloadConfigBody(allowedUser string) string {
	body := "mode = \"bot\"\nbot_token = \"secret\"\n"
	if allowedUser == "" {
		return body
	}
	return body + "allowed_user_ids = [\"" + allowedUser + "\"]\n"
}

func waitForManagerCondition(t *testing.T, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("manager condition was not met before timeout")
}

func assertAuditReloadEvent(t *testing.T, path string, source string) {
	t.Helper()

	waitForManagerCondition(t, func() bool {
		data, err := os.ReadFile(path)
		if err != nil {
			return false
		}

		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}

			var entry map[string]any
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return false
			}
			if entry["event"] != "access_policy_reloaded" {
				continue
			}

			if got, ok := entry["source"].(string); !ok || got != source {
				continue
			}
			if _, ok := entry["fields"]; ok {
				t.Fatalf("reload audit entry has nested fields: %s", line)
			}
			return true
		}

		return false
	})
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Cases ---

func TestConfigureCommandReadsTokenFromStdin(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"old\"\n")

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    strings.NewReader("new-secret\n"),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"configure", "--bot-token-stdin"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	resolved, err := config.DiscoverFrom(startDir, homeDir)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}
	if resolved.Config.BotToken != "new-secret" {
		t.Fatalf("BotToken = %q, want new-secret", resolved.Config.BotToken)
	}
}

func TestConfigureCommandRejectsConflictingTokenSources(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"old\"\n")

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    strings.NewReader("new-secret\n"),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"configure", "--bot-token", "one", "--bot-token-stdin"})
	err := cmd.Execute()
	if err == nil || err.Error() != "bot token must come from exactly one source" {
		t.Fatalf("Execute() error = %v, want conflict error", err)
	}
}

func TestReadTokenFromReader(t *testing.T) {
	t.Parallel()

	token, err := readTokenFromReader(strings.NewReader(" secret \n"))
	if err != nil {
		t.Fatalf("readTokenFromReader() error = %v", err)
	}
	if token != "secret" {
		t.Fatalf("token = %q, want secret", token)
	}
	if _, err := readTokenFromReader(strings.NewReader(" \n")); err == nil {
		t.Fatal("readTokenFromReader() error = nil, want empty token error")
	}
}

func TestSendReplyAndEditCommands(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	session := &fakeSession{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"send", "reply", "--channel", "chan-1", "--guild", "guild-1", "--message", "msg-1", "--text", "reply"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(reply) error = %v", err)
	}
	replyRequest := session.ReplyRequest()
	if replyRequest.GuildID != "guild-1" || replyRequest.ReplyToMessageID != "msg-1" {
		t.Fatalf("reply request = %#v", replyRequest)
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"send", "edit", "--channel", "chan-1", "--message", "msg-1", "--text", "edited"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(edit) error = %v", err)
	}
	editRequest := session.EditRequest()
	if editRequest.MessageID != "msg-1" || editRequest.Text != "edited" {
		t.Fatalf("edit request = %#v", editRequest)
	}
}

func TestAccessRemoveCommandsAndHelpers(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nallowed_user_ids = [\"user-1\"]\nallowed_channel_ids = [\"chan-1\"]\n")

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"access", "remove-user", "user-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(remove-user) error = %v", err)
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"access", "remove-channel", "chan-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(remove-channel) error = %v", err)
	}

	resolved, err := config.DiscoverFrom(startDir, homeDir)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}
	if slices.Contains(resolved.Config.AllowedUserIDs, "user-1") {
		t.Fatalf("AllowedUserIDs = %v, want user removed", resolved.Config.AllowedUserIDs)
	}
	if slices.Contains(resolved.Config.AllowedChannelIDs, "chan-1") {
		t.Fatalf("AllowedChannelIDs = %v, want channel removed", resolved.Config.AllowedChannelIDs)
	}

	if got := removeValue([]string{"a", "b", "a"}, "a"); len(got) != 1 || got[0] != "b" {
		t.Fatalf("removeValue() = %v, want [b]", got)
	}
}

func TestPairDenyCommand(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	state := access.DefaultState()
	now := time.Now().UTC()
	state.PendingPairs["AB12CD34"] = access.PendingPair{
		Code:      "AB12CD34",
		SenderID:  "user-1",
		ChatID:    "dm-1",
		CreatedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour),
		Nonce:     "AB12CD34:user-1",
	}
	if err := access.SaveState(config.AccessStatePath(homeDir), state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"pair", "deny", "AB12CD34"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	updated, err := access.LoadState(config.AccessStatePath(homeDir))
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if _, ok := updated.PendingPairs["AB12CD34"]; ok {
		t.Fatalf("PendingPairs still contains AB12CD34")
	}
}

func TestStateDirStatusAndDefaultEnvironment(t *testing.T) {
	t.Parallel()

	if got := stateDirStatus(filepath.Join(t.TempDir(), "missing")); got != "missing" {
		t.Fatalf("stateDirStatus(missing) = %q, want missing", got)
	}

	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if got := stateDirStatus(path); got != "invalid" {
		t.Fatalf("stateDirStatus(file) = %q, want invalid", got)
	}

	env, err := DefaultEnvironment()
	if err != nil {
		t.Fatalf("DefaultEnvironment() error = %v", err)
	}
	if env.StartDir == "" || env.HomeDir == "" {
		t.Fatalf("DefaultEnvironment() = %#v, want populated paths", env)
	}
}

func TestAccessSummaryAndPairDefaultList(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nallowed_user_ids = [\"user-1\"]\n")

	state := access.DefaultState()
	state.PendingPairs["AB12CD34"] = access.PendingPair{
		Code:      "AB12CD34",
		SenderID:  "user-1",
		ChatID:    "dm-1",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		Nonce:     "AB12CD34:user-1",
	}
	if err := access.SaveState(config.AccessStatePath(homeDir), state); err != nil {
		t.Fatalf("SaveState() error = %v", err)
	}

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"access"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(access) error = %v", err)
	}
	if !strings.Contains(env.Stdout.(*bytes.Buffer).String(), "\"pending_pair_count\": 1") {
		t.Fatalf("access summary = %q", env.Stdout.(*bytes.Buffer).String())
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"pair"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(pair) error = %v", err)
	}
	if !strings.Contains(env.Stdout.(*bytes.Buffer).String(), "AB12CD34") {
		t.Fatalf("pair list = %q", env.Stdout.(*bytes.Buffer).String())
	}
}

func TestExecuteHelp(t *testing.T) {
	t.Parallel()

	previousArgs := os.Args
	defer func() {
		os.Args = previousArgs
	}()

	os.Args = []string{"exo-discord", "help"}
	if err := Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
}

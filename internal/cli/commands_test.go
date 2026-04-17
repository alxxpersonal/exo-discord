package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeSession struct {
	mu             sync.RWMutex
	openCount      int
	closeCount     int
	sendRequest    discordpkg.SendRequest
	replyRequest   discordpkg.ReplyRequest
	reactRequest   discordpkg.ReactRequest
	editRequest    discordpkg.EditRequest
	historyRequest discordpkg.HistoryRequest
	downloadReq    discordpkg.DownloadRequest
	statusRequest  discordpkg.StatusRequest
	handler        discordpkg.InboundHandler
}

func (f *fakeSession) Open(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openCount++
	return nil
}

func (f *fakeSession) Close(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeCount++
	return nil
}

func (f *fakeSession) Mode() string {
	return "bot"
}

func (f *fakeSession) SendMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "msg-send"}, nil
}

func (f *fakeSession) Reply(_ context.Context, req discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replyRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "msg-reply"}, nil
}

func (f *fakeSession) React(_ context.Context, req discordpkg.ReactRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reactRequest = req
	return nil
}

func (f *fakeSession) EditMessage(_ context.Context, req discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.editRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) FetchHistory(_ context.Context, req discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.historyRequest = req
	return nil, nil
}

func (f *fakeSession) DownloadAttachments(_ context.Context, req discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.downloadReq = req
	return nil, nil
}

func (f *fakeSession) SetStatus(_ context.Context, req discordpkg.StatusRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statusRequest = req
	return nil
}

func (f *fakeSession) Subscribe(handler discordpkg.InboundHandler) func() {
	f.mu.Lock()
	f.handler = handler
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		f.handler = nil
		f.mu.Unlock()
	}
}

func (f *fakeSession) emit(ctx context.Context, message discordpkg.Message) {
	f.mu.RLock()
	handler := f.handler
	f.mu.RUnlock()
	if handler != nil {
		handler(ctx, message)
	}
}

func (f *fakeSession) OpenCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.openCount
}

func (f *fakeSession) CloseCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.closeCount
}

func (f *fakeSession) SendRequest() discordpkg.SendRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.sendRequest
}

func (f *fakeSession) ReplyRequest() discordpkg.ReplyRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.replyRequest
}

func (f *fakeSession) ReactRequest() discordpkg.ReactRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.reactRequest
}

func (f *fakeSession) EditRequest() discordpkg.EditRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.editRequest
}

func (f *fakeSession) StatusRequest() discordpkg.StatusRequest {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.statusRequest
}

type fakeMCPServer struct {
	runCount int
}

func (f *fakeMCPServer) Run(context.Context) error {
	f.runCount++
	return nil
}

type fakeListenRunner struct {
	runCount int
	request  listenRequest
}

func (f *fakeListenRunner) Run(_ context.Context, req listenRequest) error {
	f.runCount++
	f.request = req
	return nil
}

type fakeBotModeRunner struct {
	runCount int
	request  botModeRequest
}

func (f *fakeBotModeRunner) Run(_ context.Context, req botModeRequest) error {
	f.runCount++
	f.request = req
	return nil
}

// --- Test Cases ---

func TestRootCommandRegistersSpecCommands(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(Environment{
		StartDir: t.TempDir(),
		HomeDir:  t.TempDir(),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})

	names := make([]string, 0, len(cmd.Commands()))
	for _, child := range cmd.Commands() {
		names = append(names, child.Name())
	}

	required := []string{
		"init",
		"configure",
		"doctor",
		"send",
		"listen",
		"bot-mode",
		"access",
		"pair",
		"mcp",
		"user-install-mode",
	}
	for _, name := range required {
		if !slices.Contains(names, name) {
			t.Fatalf("root commands = %v, missing %q", names, name)
		}
	}
}

func TestConfigureCommandUpdatesConfig(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"old-secret\"\nhook.kind = \"none\"\n")

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{
		"configure",
		"--bot-token", "new-secret",
		"--hook-kind", "http",
		"--hook-http-url", "http://127.0.0.1:8787/hook",
		"--require-mention=false",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	stdout := env.Stdout.(*bytes.Buffer).String()
	if strings.Contains(stdout, "old-secret") || strings.Contains(stdout, "new-secret") {
		t.Fatalf("configure output leaked token: %q", stdout)
	}

	var result configureResult
	if err := json.Unmarshal(env.Stdout.(*bytes.Buffer).Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !result.OK {
		t.Fatalf("configure result = %#v, want ok", result)
	}

	resolved, err := config.DiscoverFrom(startDir, homeDir)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}
	if resolved.Config.BotToken != "new-secret" {
		t.Fatalf("BotToken = %q, want new-secret", resolved.Config.BotToken)
	}
	if resolved.Config.Hook.Kind != config.HookKindHTTP {
		t.Fatalf("Hook.Kind = %q, want http", resolved.Config.Hook.Kind)
	}
	if resolved.Config.Hook.HTTP.URL != "http://127.0.0.1:8787/hook" {
		t.Fatalf("Hook.HTTP.URL = %q", resolved.Config.Hook.HTTP.URL)
	}
	if resolved.Config.RequireMention {
		t.Fatalf("RequireMention = %t, want false", resolved.Config.RequireMention)
	}
}

func TestSendCommandUsesSessionFactory(t *testing.T) {
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
	cmd.SetArgs([]string{"send", "--channel", "chan-1", "--text", "hello"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	sendRequest := session.SendRequest()
	if sendRequest.ChannelID != "chan-1" || sendRequest.Text != "hello" {
		t.Fatalf("send request = %#v", sendRequest)
	}

	var result sendResult
	if err := json.Unmarshal(env.Stdout.(*bytes.Buffer).Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if result.MessageID != "msg-send" {
		t.Fatalf("send result = %#v, want msg-send", result)
	}
}

func TestSendReactCommandUsesSessionFactory(t *testing.T) {
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
	cmd.SetArgs([]string{"send", "react", "--channel", "chan-1", "--message", "msg-1", "--emoji", "👍"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	reactRequest := session.ReactRequest()
	if reactRequest.MessageID != "msg-1" || reactRequest.Emoji != "👍" {
		t.Fatalf("react request = %#v", reactRequest)
	}
}

func TestAccessCommandsUpdateConfigAndListPairedUsers(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	state := access.DefaultState()
	state.ApprovedUsers["221773638772129792"] = access.ApprovedUser{
		ApprovedAt: time.Unix(1_700_000_000, 0).UTC(),
		Source:     "pairing",
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
	cmd.SetArgs([]string{"access", "allow-user", "221773638772129792"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(allow-user) error = %v", err)
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"access", "allow-channel", "846209781206941736"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(allow-channel) error = %v", err)
	}

	resolved, err := config.DiscoverFrom(startDir, homeDir)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}
	if !slices.Contains(resolved.Config.AllowedUserIDs, "221773638772129792") {
		t.Fatalf("AllowedUserIDs = %v", resolved.Config.AllowedUserIDs)
	}
	if !slices.Contains(resolved.Config.AllowedChannelIDs, "846209781206941736") {
		t.Fatalf("AllowedChannelIDs = %v", resolved.Config.AllowedChannelIDs)
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"access", "list-paired"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(list-paired) error = %v", err)
	}

	var result pairedUsersResult
	if err := json.Unmarshal(env.Stdout.(*bytes.Buffer).Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got, want := len(result.PairedUsers), 1; got != want {
		t.Fatalf("paired users count = %d, want %d", got, want)
	}
	if result.PairedUsers[0].UserID != "221773638772129792" {
		t.Fatalf("paired user = %#v", result.PairedUsers[0])
	}
}

func TestPairListAndApproveCommands(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	state := access.DefaultState()
	now := time.Now().UTC()
	state.PendingPairs["AB12CD34"] = access.PendingPair{
		Code:        "AB12CD34",
		SenderID:    "221773638772129792",
		ChatID:      "dm-1",
		CreatedAt:   now.Add(-5 * time.Minute),
		ExpiresAt:   now.Add(time.Hour),
		ResendCount: 1,
		Nonce:       "AB12CD34:221773638772129792",
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
	cmd.SetArgs([]string{"pair", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(list) error = %v", err)
	}

	var listResult pendingPairsResult
	if err := json.Unmarshal(env.Stdout.(*bytes.Buffer).Bytes(), &listResult); err != nil {
		t.Fatalf("Unmarshal(list) error = %v", err)
	}
	if got, want := len(listResult.PendingPairs), 1; got != want {
		t.Fatalf("pending pairs = %d, want %d", got, want)
	}

	env.Stdout.(*bytes.Buffer).Reset()
	cmd = NewRootCommand(env)
	cmd.SetArgs([]string{"pair", "approve", "AB12CD34"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(approve) error = %v", err)
	}

	updated, err := access.LoadState(config.AccessStatePath(homeDir))
	if err != nil {
		t.Fatalf("LoadState() error = %v", err)
	}
	if _, ok := updated.ApprovedUsers["221773638772129792"]; !ok {
		t.Fatalf("ApprovedUsers = %v", updated.ApprovedUsers)
	}
	if _, ok := updated.PendingPairs["AB12CD34"]; ok {
		t.Fatalf("PendingPairs still contains AB12CD34")
	}
}

func TestMCPServeCommandOpensSessionAndRunsServer(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\nmcp_enabled = true\n")

	session := &fakeSession{}
	server := &fakeMCPServer{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		NewMCPServer: func(discordpkg.Session) mcpServer {
			return server
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"mcp", "serve"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	openCount := session.OpenCount()
	closeCount := session.CloseCount()
	if openCount != 1 || closeCount != 1 {
		t.Fatalf("session open=%d close=%d, want 1/1", openCount, closeCount)
	}
	if server.runCount != 1 {
		t.Fatalf("server run count = %d, want 1", server.runCount)
	}
	if !strings.Contains(env.Stderr.(*bytes.Buffer).String(), "serving mcp over stdio") {
		t.Fatalf("stderr = %q", env.Stderr.(*bytes.Buffer).String())
	}
}

func TestListenCommandDryRunSkipsSessionCreation(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	sessionCreated := false
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			sessionCreated = true
			return &fakeSession{}, nil
		},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"listen", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if sessionCreated {
		t.Fatal("listen dry-run created a session")
	}

	var result modeDryRunResult
	if err := json.Unmarshal(env.Stdout.(*bytes.Buffer).Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if result.Command != "listen" || !result.DryRun {
		t.Fatalf("listen dry-run result = %#v", result)
	}
}

func TestListenCommandUsesInjectedRunner(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	session := &fakeSession{}
	runner := &fakeListenRunner{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		ListenRunner: runner,
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"listen"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if runner.runCount != 1 {
		t.Fatalf("runner run count = %d, want 1", runner.runCount)
	}
	if runner.request.Session != session {
		t.Fatalf("runner session = %#v, want fake session", runner.request.Session)
	}
}

func TestBotModeCommandUsesInjectedRunner(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, "mode = \"bot\"\nbot_token = \"secret\"\n")

	session := &fakeSession{}
	runner := &fakeBotModeRunner{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
		NewSession: func(config.ResolvedConfig) (discordpkg.Session, error) {
			return session, nil
		},
		BotModeRunner: runner,
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"bot-mode"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if runner.runCount != 1 {
		t.Fatalf("runner run count = %d, want 1", runner.runCount)
	}
	if runner.request.Session != session {
		t.Fatalf("runner session = %#v, want fake session", runner.request.Session)
	}
}

func TestUserInstallModeCommandReturnsReservedError(t *testing.T) {
	t.Parallel()

	cmd := NewRootCommand(Environment{
		StartDir: t.TempDir(),
		HomeDir:  t.TempDir(),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	})
	cmd.SetArgs([]string{"user-install-mode"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want reserved mode error")
	}
	if got, want := err.Error(), "user_install mode is not implemented"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// --- Helpers ---

func setupWorkspace(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	startDir := filepath.Join(root, "workspace")
	homeDir := filepath.Join(root, "home")
	//nolint:gosec
	if err := os.MkdirAll(startDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(startDir) error = %v", err)
	}
	//nolint:gosec
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(homeDir) error = %v", err)
	}
	return startDir, homeDir
}

func writeProjectConfig(t *testing.T, startDir string, body string) string {
	t.Helper()

	path := filepath.Join(startDir, ".exo-discord")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
	return path
}

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/discord/userinstall"
)

// --- Test Cases ---

func TestAuthLoginExchangesCodeAndPersistsToken(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-1","token_type":"Bearer","expires_in":604800,"refresh_token":"refresh-1","scope":"identify guilds"}`))
	}))
	defer tokenServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"221","username":"alxx"}`))
	}))
	defer apiServer.Close()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, oauthConfigBody(tokenServer.URL))

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    strings.NewReader(""),
		Stdout:   stdout,
		Stderr:   stderr,
		Context:  context.Background,
		OAuthOverride: oauthEndpointOverride{
			TokenBase:  tokenServer.URL,
			RevokeBase: tokenServer.URL,
		},
		OAuthAPIBase: apiServer.URL,
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"auth", "login", "--code", "test-code"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v\nstderr=%s", err, stderr.String())
	}

	var result authLoginResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v\nstdout=%s", err, stdout.String())
	}
	if result.UserID != "221" || result.Username != "alxx" {
		t.Fatalf("result = %#v", result)
	}
	if strings.Contains(stdout.String(), "access-1") || strings.Contains(stdout.String(), "refresh-1") {
		t.Fatalf("auth login leaked tokens: %q", stdout.String())
	}

	storage, err := userinstall.NewStorage(filepath.Join(homeDir, ".exo-discord", "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	loaded, err := storage.Load("221")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.AccessToken != "access-1" {
		t.Fatalf("AccessToken = %q, want access-1", loaded.AccessToken)
	}
}

func TestAuthListReturnsStoredTokens(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, oauthConfigBody("http://x"))

	storage, err := userinstall.NewStorage(filepath.Join(homeDir, ".exo-discord", "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if err := storage.Save(userinstall.StoredToken{
		UserID:      "221",
		Username:    "alxx",
		AccessToken: "access-1",
		Scope:       "identify guilds",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	stdout := &bytes.Buffer{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   stdout,
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"auth", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var result authListResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if len(result.Records) != 1 || result.Records[0].UserID != "221" {
		t.Fatalf("records = %#v", result.Records)
	}
	if strings.Contains(stdout.String(), "access-1") {
		t.Fatalf("auth list leaked access token: %q", stdout.String())
	}
}

func TestAuthRevokeRequiresConfirmationAndDeletes(t *testing.T) {
	t.Parallel()

	revokeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer revokeServer.Close()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, oauthConfigBody(revokeServer.URL))

	storage, err := userinstall.NewStorage(filepath.Join(homeDir, ".exo-discord", "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if err := storage.Save(userinstall.StoredToken{
		UserID:       "221",
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// --yes skips prompt
	stdout := &bytes.Buffer{}
	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   stdout,
		Stderr:   &bytes.Buffer{},
	}

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"auth", "revoke", "--yes", "221"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if _, err := storage.Load("221"); err == nil {
		t.Fatal("Load() error = nil, want token deleted")
	}

	var result authRevokeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if !result.OK || result.UserID != "221" {
		t.Fatalf("result = %#v", result)
	}
}

func TestAuthRevokeRejectsUnconfirmed(t *testing.T) {
	t.Parallel()

	startDir, homeDir := setupWorkspace(t)
	writeProjectConfig(t, startDir, oauthConfigBody("http://x"))

	storage, err := userinstall.NewStorage(filepath.Join(homeDir, ".exo-discord", "oauth"))
	if err != nil {
		t.Fatalf("NewStorage() error = %v", err)
	}
	if err := storage.Save(userinstall.StoredToken{
		UserID:      "221",
		AccessToken: "access-1",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    strings.NewReader("wrong-id\n"),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}
	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"auth", "revoke", "221"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("Execute() error = nil, want cancellation")
	}
	if _, err := storage.Load("221"); err != nil {
		t.Fatalf("token gone after cancelled revoke: %v", err)
	}
}

func TestUserInstallModeCommandHint(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	env := Environment{
		StartDir: t.TempDir(),
		HomeDir:  t.TempDir(),
		Stdout:   stdout,
		Stderr:   &bytes.Buffer{},
	}
	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"user-install-mode"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "auth login") {
		t.Fatalf("stdout = %q, want auth login hint", stdout.String())
	}
}

func TestParseCallbackHandlesURLAndPairs(t *testing.T) {
	t.Parallel()

	code, state := parseCallback("http://127.0.0.1:8080/cb?code=abc&state=xyz")
	if code != "abc" || state != "xyz" {
		t.Fatalf("parseCallback(url) = %q,%q", code, state)
	}
	code, state = parseCallback("code=abc state=xyz")
	if code != "abc" || state != "xyz" {
		t.Fatalf("parseCallback(pairs) = %q,%q", code, state)
	}
	code, state = parseCallback("just-a-code")
	if code != "just-a-code" || state != "" {
		t.Fatalf("parseCallback(bare) = %q,%q", code, state)
	}
}

func TestSplitScope(t *testing.T) {
	t.Parallel()

	if got := splitScope(""); got != nil {
		t.Fatalf("splitScope(empty) = %v", got)
	}
	got := splitScope("identify  guilds")
	if len(got) != 2 || got[0] != "identify" || got[1] != "guilds" {
		t.Fatalf("splitScope() = %v", got)
	}
}

func TestRequireOAuthConfigValidates(t *testing.T) {
	t.Parallel()

	if _, err := requireOAuthConfig(config.ResolvedConfig{}); err == nil {
		t.Fatal("requireOAuthConfig(empty) error = nil")
	}
	if _, err := requireOAuthConfig(config.ResolvedConfig{
		Config: config.Config{OAuth: config.OAuthConfig{ClientID: "c"}},
	}); err == nil {
		t.Fatal("requireOAuthConfig(partial) error = nil")
	}
	cfg, err := requireOAuthConfig(config.ResolvedConfig{
		Config: config.Config{
			OAuth: config.OAuthConfig{
				ClientID:     "c",
				ClientSecret: "s",
				RedirectURI:  "http://x",
			},
		},
	})
	if err != nil {
		t.Fatalf("requireOAuthConfig() error = %v", err)
	}
	if len(cfg.Scopes) == 0 {
		t.Fatalf("cfg.Scopes = nil, want default scopes")
	}
}

// --- Helpers ---

func oauthConfigBody(redirectBase string) string {
	return strings.Join([]string{
		"mode = \"user_install\"",
		"bot_token = \"unused\"",
		"[oauth]",
		"client_id = \"client-1\"",
		"client_secret = \"client-secret\"",
		"redirect_uri = \"" + redirectBase + "/callback\"",
		"scopes = [\"identify\", \"guilds\"]",
		"",
	}, "\n")
}

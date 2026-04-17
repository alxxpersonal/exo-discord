package userinstall

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// --- Test Cases ---

func TestAuthorizationURLIncludesRequiredParams(t *testing.T) {
	t.Parallel()

	client := NewOAuthClient(OAuthConfig{
		ClientID:    "client-1",
		RedirectURI: "http://127.0.0.1:8080/callback",
		Scopes:      []string{"identify", "guilds"},
	}, nil)

	raw, state, err := client.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	if state == "" {
		t.Fatal("state = empty")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse() error = %v", err)
	}
	values := parsed.Query()
	for _, pair := range [][2]string{
		{"response_type", "code"},
		{"client_id", "client-1"},
		{"redirect_uri", "http://127.0.0.1:8080/callback"},
		{"scope", "identify guilds"},
		{"integration_type", "1"},
	} {
		if got := values.Get(pair[0]); got != pair[1] {
			t.Fatalf("query %s = %q, want %q", pair[0], got, pair[1])
		}
	}
	if values.Get("state") != state {
		t.Fatalf("state mismatch: %q vs %q", values.Get("state"), state)
	}
}

func TestAuthorizationURLValidatesInputs(t *testing.T) {
	t.Parallel()

	client := NewOAuthClient(OAuthConfig{}, nil)
	if _, _, err := client.AuthorizationURL(); err == nil {
		t.Fatal("AuthorizationURL() error = nil, want missing client id")
	}

	client = NewOAuthClient(OAuthConfig{ClientID: "client"}, nil)
	if _, _, err := client.AuthorizationURL(); err == nil {
		t.Fatal("AuthorizationURL() error = nil, want missing redirect uri")
	}

	client = NewOAuthClient(OAuthConfig{ClientID: "client", RedirectURI: "http://x"}, nil)
	if _, _, err := client.AuthorizationURL(); err == nil {
		t.Fatal("AuthorizationURL() error = nil, want missing scopes")
	}
}

func TestConsumeStateRejectsUnknownAndExpired(t *testing.T) {
	t.Parallel()

	client := NewOAuthClient(OAuthConfig{
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1/cb",
		Scopes:      []string{"identify"},
	}, nil)

	if err := client.ConsumeState(""); err == nil {
		t.Fatal("ConsumeState(empty) error = nil, want empty state")
	}
	if err := client.ConsumeState("unknown"); err == nil {
		t.Fatal("ConsumeState(unknown) error = nil, want unknown state")
	}

	_, state, err := client.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	if err := client.ConsumeState(state); err != nil {
		t.Fatalf("ConsumeState(valid) error = %v", err)
	}
	// second consume fails
	if err := client.ConsumeState(state); err == nil {
		t.Fatal("ConsumeState(reused) error = nil, want rejection")
	}

	// expired
	_, expiredState, err := client.AuthorizationURL()
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	base := time.Now()
	client.clock = func() time.Time { return base.Add(30 * time.Minute) }
	if err := client.ConsumeState(expiredState); err == nil {
		t.Fatal("ConsumeState(expired) error = nil, want expiry rejection")
	}
}

func TestExchangeCodePostsAndParsesToken(t *testing.T) {
	t.Parallel()

	var capturedBody string
	var capturedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-1",
			"token_type":    "Bearer",
			"expires_in":    604800,
			"refresh_token": "refresh-1",
			"scope":         "identify guilds",
		})
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURI:  "http://127.0.0.1/cb",
		Scopes:       []string{"identify"},
		TokenBase:    server.URL,
	}, server.Client())

	token, err := client.ExchangeCode(context.Background(), "code-xyz")
	if err != nil {
		t.Fatalf("ExchangeCode() error = %v", err)
	}
	if token.AccessToken != "access-1" {
		t.Fatalf("AccessToken = %q, want access-1", token.AccessToken)
	}
	if token.RefreshToken != "refresh-1" {
		t.Fatalf("RefreshToken = %q, want refresh-1", token.RefreshToken)
	}
	if !strings.Contains(capturedBody, "grant_type=authorization_code") {
		t.Fatalf("body = %q, want grant_type=authorization_code", capturedBody)
	}
	if !strings.Contains(capturedBody, "code=code-xyz") {
		t.Fatalf("body = %q, want code=code-xyz", capturedBody)
	}
	if !strings.HasPrefix(capturedAuth, "Basic ") {
		t.Fatalf("Authorization = %q, want basic auth", capturedAuth)
	}
}

func TestExchangeCodeValidatesInputs(t *testing.T) {
	t.Parallel()

	client := NewOAuthClient(OAuthConfig{ClientID: "c", ClientSecret: "s", RedirectURI: "r", Scopes: []string{"s"}}, nil)
	if _, err := client.ExchangeCode(context.Background(), ""); err == nil {
		t.Fatal("ExchangeCode(empty) error = nil, want empty code")
	}

	missing := NewOAuthClient(OAuthConfig{RedirectURI: "r", Scopes: []string{"s"}}, nil)
	if _, err := missing.ExchangeCode(context.Background(), "code"); err == nil {
		t.Fatal("ExchangeCode(missing client) error = nil, want missing creds")
	}
}

func TestExchangeCodeErrorsOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		RedirectURI:  "http://x",
		Scopes:       []string{"identify"},
		TokenBase:    server.URL,
	}, server.Client())
	if _, err := client.ExchangeCode(context.Background(), "bad"); err == nil {
		t.Fatal("ExchangeCode() error = nil, want status error")
	}
}

func TestRefreshTokenPostsRefreshGrant(t *testing.T) {
	t.Parallel()

	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-2","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-2","scope":"identify"}`))
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		RedirectURI:  "http://x",
		Scopes:       []string{"identify"},
		TokenBase:    server.URL,
	}, server.Client())

	token, err := client.RefreshToken(context.Background(), "refresh-1")
	if err != nil {
		t.Fatalf("RefreshToken() error = %v", err)
	}
	if token.AccessToken != "access-2" {
		t.Fatalf("AccessToken = %q, want access-2", token.AccessToken)
	}
	if !strings.Contains(captured, "grant_type=refresh_token") {
		t.Fatalf("body = %q, want refresh grant", captured)
	}
	if _, err := client.RefreshToken(context.Background(), ""); err == nil {
		t.Fatal("RefreshToken(empty) error = nil, want error")
	}
}

func TestRevokeTokenPostsRevocation(t *testing.T) {
	t.Parallel()

	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		RedirectURI:  "http://x",
		Scopes:       []string{"identify"},
		RevokeBase:   server.URL,
	}, server.Client())

	if err := client.RevokeToken(context.Background(), "token-1", "access_token"); err != nil {
		t.Fatalf("RevokeToken() error = %v", err)
	}
	if !strings.Contains(captured, "token=token-1") {
		t.Fatalf("body = %q, want token=token-1", captured)
	}
	if !strings.Contains(captured, "token_type_hint=access_token") {
		t.Fatalf("body = %q, want token_type_hint", captured)
	}
	if err := client.RevokeToken(context.Background(), "", ""); err == nil {
		t.Fatal("RevokeToken(empty) error = nil, want empty token error")
	}
}

func TestRevokeTokenErrorsOnNon200(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_token"}`))
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		RedirectURI:  "http://x",
		Scopes:       []string{"identify"},
		RevokeBase:   server.URL,
	}, server.Client())
	if err := client.RevokeToken(context.Background(), "token", ""); err == nil {
		t.Fatal("RevokeToken() error = nil, want status error")
	}
}

func TestTokenResponseRejectsMissingAccessToken(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token_type":"Bearer","expires_in":100}`))
	}))
	defer server.Close()

	client := NewOAuthClient(OAuthConfig{
		ClientID:     "c",
		ClientSecret: "s",
		RedirectURI:  "http://x",
		Scopes:       []string{"identify"},
		TokenBase:    server.URL,
	}, server.Client())
	if _, err := client.ExchangeCode(context.Background(), "code"); err == nil {
		t.Fatal("ExchangeCode() error = nil, want missing access_token error")
	}
}

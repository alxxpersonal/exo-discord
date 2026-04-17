package userinstall

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// --- Endpoints ---

const (
	// AuthorizeEndpoint is the Discord OAuth 2.0 authorize URL.
	AuthorizeEndpoint = "https://discord.com/oauth2/authorize"
	// TokenEndpoint is the Discord OAuth 2.0 token URL.
	TokenEndpoint = "https://discord.com/api/oauth2/token"
	// RevokeEndpoint is the Discord OAuth 2.0 token revoke URL.
	RevokeEndpoint = "https://discord.com/api/oauth2/token/revoke"
	// APIBase is the Discord REST v10 base URL.
	APIBase = "https://discord.com/api/v10"
)

// --- Types ---

// OAuthConfig stores the OAuth 2.0 client configuration.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
	// AuthorizeBase overrides AuthorizeEndpoint when non-empty (for tests).
	AuthorizeBase string
	// TokenBase overrides TokenEndpoint when non-empty (for tests).
	TokenBase string
	// RevokeBase overrides RevokeEndpoint when non-empty (for tests).
	RevokeBase string
}

// TokenResponse stores the parsed Discord token endpoint response.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// OAuthClient performs Discord OAuth 2.0 authorization code exchange and refresh.
type OAuthClient struct {
	cfg    OAuthConfig
	http   *http.Client
	states *stateStore
	clock  func() time.Time
}

// --- Constructors ---

// NewOAuthClient creates an OAuth client with sane defaults.
func NewOAuthClient(cfg OAuthConfig, httpClient *http.Client) *OAuthClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthClient{
		cfg:    cfg,
		http:   httpClient,
		states: newStateStore(10 * time.Minute),
		clock:  time.Now,
	}
}

// --- Authorization URL ---

// AuthorizationURL builds the Discord authorize URL and returns the generated csrf state.
func (c *OAuthClient) AuthorizationURL() (string, string, error) {
	if strings.TrimSpace(c.cfg.ClientID) == "" {
		return "", "", fmt.Errorf("oauth client id is required")
	}
	if strings.TrimSpace(c.cfg.RedirectURI) == "" {
		return "", "", fmt.Errorf("oauth redirect uri is required")
	}
	if len(c.cfg.Scopes) == 0 {
		return "", "", fmt.Errorf("oauth scopes must not be empty")
	}

	state, err := generateState()
	if err != nil {
		return "", "", err
	}
	c.states.put(state, c.clock())

	base := c.cfg.AuthorizeBase
	if base == "" {
		base = AuthorizeEndpoint
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", c.cfg.ClientID)
	values.Set("scope", strings.Join(c.cfg.Scopes, " "))
	values.Set("redirect_uri", c.cfg.RedirectURI)
	values.Set("state", state)
	values.Set("integration_type", "1")
	values.Set("prompt", "consent")

	return base + "?" + values.Encode(), state, nil
}

// ConsumeState validates and removes a returned csrf state.
func (c *OAuthClient) ConsumeState(state string) error {
	if strings.TrimSpace(state) == "" {
		return fmt.Errorf("oauth state must not be empty")
	}
	if !c.states.consume(state, c.clock()) {
		return fmt.Errorf("oauth state is unknown or expired")
	}
	return nil
}

// --- Token Exchange ---

// ExchangeCode exchanges an authorization code for a token response.
func (c *OAuthClient) ExchangeCode(ctx context.Context, code string) (TokenResponse, error) {
	if strings.TrimSpace(code) == "" {
		return TokenResponse{}, fmt.Errorf("oauth code must not be empty")
	}

	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", c.cfg.RedirectURI)

	return c.postToken(ctx, values)
}

// RefreshToken exchanges a refresh token for a new token response.
func (c *OAuthClient) RefreshToken(ctx context.Context, refreshToken string) (TokenResponse, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return TokenResponse{}, fmt.Errorf("oauth refresh token must not be empty")
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)

	return c.postToken(ctx, values)
}

// RevokeToken revokes an access or refresh token.
func (c *OAuthClient) RevokeToken(ctx context.Context, token string, hint string) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("oauth token must not be empty")
	}

	values := url.Values{}
	values.Set("token", token)
	if hint != "" {
		values.Set("token_type_hint", hint)
	}

	base := c.cfg.RevokeBase
	if base == "" {
		base = RevokeEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, strings.NewReader(values.Encode()))
	if err != nil {
		return fmt.Errorf("build revoke request: %w", err)
	}
	c.applyClientAuth(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("post revoke: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("revoke returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// --- Internal Helpers ---

func (c *OAuthClient) postToken(ctx context.Context, values url.Values) (TokenResponse, error) {
	if strings.TrimSpace(c.cfg.ClientID) == "" || strings.TrimSpace(c.cfg.ClientSecret) == "" {
		return TokenResponse{}, fmt.Errorf("oauth client id and secret are required")
	}

	base := c.cfg.TokenBase
	if base == "" {
		base = TokenEndpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base, strings.NewReader(values.Encode()))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("build token request: %w", err)
	}
	c.applyClientAuth(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("post token: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("read token body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return TokenResponse{}, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return TokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}
	if strings.TrimSpace(token.AccessToken) == "" {
		return TokenResponse{}, fmt.Errorf("token response missing access_token")
	}
	return token, nil
}

func (c *OAuthClient) applyClientAuth(req *http.Request) {
	req.SetBasicAuth(c.cfg.ClientID, c.cfg.ClientSecret)
}

// --- State Store ---

type stateStore struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]time.Time
}

func newStateStore(ttl time.Duration) *stateStore {
	return &stateStore{
		ttl:   ttl,
		items: make(map[string]time.Time),
	}
}

func (s *stateStore) put(state string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(now)
	s.items[state] = now
}

func (s *stateStore) consume(state string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(now)

	created, ok := s.items[state]
	if !ok {
		return false
	}
	delete(s.items, state)
	return now.Sub(created) <= s.ttl
}

func (s *stateStore) expireLocked(now time.Time) {
	for key, created := range s.items {
		if now.Sub(created) > s.ttl {
			delete(s.items, key)
		}
	}
}

// --- Random Helpers ---

func generateState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random for state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

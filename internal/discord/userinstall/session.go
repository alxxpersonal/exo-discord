package userinstall

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Errors ---

// ErrNotSupported reports that the user-install mode does not support the call.
var ErrNotSupported = errors.New("operation not supported in user_install mode")

// --- Types ---

// TokenRefresher refreshes an expiring oauth token.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, refreshToken string) (TokenResponse, error)
}

// Session adapts a user-auth bearer token to the discord.Session interface.
//
// User-install mode is read-only against the Discord REST API. Outbound message,
// reply, react, edit, download, and status operations return ErrNotSupported.
type Session struct {
	userID      string
	storage     *Storage
	refresher   TokenRefresher
	rest        atomic.Pointer[RESTClient]
	mu          sync.RWMutex
	token       StoredToken
	subscribers map[uint64]discord.InboundHandler
	nextSubID   uint64
	refreshLead time.Duration
	clock       func() time.Time
	restFactory func(bearer string) *RESTClient
}

// Config stores the UserSession construction values.
type Config struct {
	UserID      string
	Storage     *Storage
	Refresher   TokenRefresher
	HTTPClient  *http.Client
	APIBase     string
	RefreshLead time.Duration
}

// --- Constructors ---

// NewSession creates a user-install Session for a stored token.
func NewSession(cfg Config) (*Session, error) {
	if cfg.Storage == nil {
		return nil, fmt.Errorf("user-install session requires storage")
	}
	if cfg.Refresher == nil {
		return nil, fmt.Errorf("user-install session requires token refresher")
	}
	if cfg.UserID == "" {
		return nil, fmt.Errorf("user-install session requires user id")
	}

	token, err := cfg.Storage.Load(cfg.UserID)
	if err != nil {
		return nil, err
	}
	if token.UserID != "" && token.UserID != cfg.UserID {
		return nil, fmt.Errorf("oauth token user id %q does not match config user id %q", token.UserID, cfg.UserID)
	}

	lead := cfg.RefreshLead
	if lead <= 0 {
		lead = 5 * time.Minute
	}

	restFactory := func(bearer string) *RESTClient {
		return NewRESTClient(cfg.HTTPClient, cfg.APIBase, bearer)
	}

	sess := &Session{
		userID:      cfg.UserID,
		storage:     cfg.Storage,
		refresher:   cfg.Refresher,
		token:       token,
		subscribers: make(map[uint64]discord.InboundHandler),
		refreshLead: lead,
		clock:       time.Now,
		restFactory: restFactory,
	}
	sess.rest.Store(restFactory(token.AccessToken))
	return sess, nil
}

// --- Lifecycle ---

// Open verifies the token by calling GET /users/@me, refreshing if expired.
func (s *Session) Open(ctx context.Context) error {
	if err := s.ensureFresh(ctx); err != nil {
		return err
	}
	rest := s.rest.Load()
	if rest == nil {
		return fmt.Errorf("user-install session not initialized")
	}
	if _, err := rest.CurrentUser(ctx); err != nil {
		return fmt.Errorf("verify user-install token: %w", err)
	}
	return nil
}

// Close releases the session. No persistent connection to tear down.
func (s *Session) Close(context.Context) error {
	return nil
}

// Mode returns the session mode name.
func (s *Session) Mode() string {
	return "user_install"
}

// --- Message Operations ---

// SendMessage is not supported in user-install mode.
func (s *Session) SendMessage(context.Context, discord.SendRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, ErrNotSupported
}

// Reply is not supported in user-install mode.
func (s *Session) Reply(context.Context, discord.ReplyRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, ErrNotSupported
}

// React is not supported in user-install mode.
func (s *Session) React(context.Context, discord.ReactRequest) error {
	return ErrNotSupported
}

// EditMessage is not supported in user-install mode.
func (s *Session) EditMessage(context.Context, discord.EditRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, ErrNotSupported
}

// FetchHistory is not supported in user-install mode.
func (s *Session) FetchHistory(context.Context, discord.HistoryRequest) ([]discord.Message, error) {
	return nil, ErrNotSupported
}

// DownloadAttachments is not supported in user-install mode.
func (s *Session) DownloadAttachments(context.Context, discord.DownloadRequest) ([]discord.DownloadedFile, error) {
	return nil, ErrNotSupported
}

// SetStatus is not supported in user-install mode.
func (s *Session) SetStatus(context.Context, discord.StatusRequest) error {
	return ErrNotSupported
}

// Subscribe registers an inbound handler. User-install mode never emits events
// (no gateway), so handlers are stored but never called.
func (s *Session) Subscribe(handler discord.InboundHandler) func() {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := s.nextSubID
	s.nextSubID++
	s.subscribers[id] = handler

	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subscribers, id)
	}
}

// --- Token Helpers ---

// ensureFresh refreshes the oauth token if it is within refreshLead of expiring.
func (s *Session) ensureFresh(ctx context.Context) error {
	s.mu.RLock()
	token := s.token
	s.mu.RUnlock()

	if token.ExpiresAt.IsZero() || s.clock().Add(s.refreshLead).Before(token.ExpiresAt) {
		return nil
	}
	if token.RefreshToken == "" {
		return fmt.Errorf("oauth token expired and no refresh token available")
	}

	refreshed, err := s.refresher.RefreshToken(ctx, token.RefreshToken)
	if err != nil {
		return fmt.Errorf("refresh oauth token: %w", err)
	}

	now := s.clock()
	updated := StoredToken{
		UserID:       token.UserID,
		Username:     token.Username,
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		TokenType:    refreshed.TokenType,
		Scope:        refreshed.Scope,
		ExpiresAt:    now.Add(time.Duration(refreshed.ExpiresIn) * time.Second),
		ObtainedAt:   now,
	}
	if updated.RefreshToken == "" {
		// discord rotates refresh tokens; preserve the old one only if a rotation did not happen
		updated.RefreshToken = token.RefreshToken
	}
	if updated.TokenType == "" {
		updated.TokenType = token.TokenType
	}
	if updated.Scope == "" {
		updated.Scope = token.Scope
	}

	if err := s.storage.Save(updated); err != nil {
		return err
	}

	s.mu.Lock()
	s.token = updated
	s.mu.Unlock()
	s.rest.Store(s.restFactory(updated.AccessToken))
	return nil
}

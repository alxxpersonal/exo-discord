package access

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/audit"
)

// --- Types ---

// Manager evaluates access policy and persists pairing state.
type Manager struct {
	mu         sync.RWMutex
	state      State
	statePath  string
	policy     Policy
	configPath string
	watcher    *Watcher
	logger     *slog.Logger
	auditor    *audit.Logger
	now        func() time.Time
	newCode    func() (string, error)
}

// --- Constructors ---

// NewManager creates an access manager for a state path.
func NewManager(statePath string, policy Policy) (*Manager, error) {
	state, err := LoadState(statePath)
	if err != nil {
		return nil, err
	}

	manager := &Manager{
		state:     state,
		statePath: statePath,
		policy:    policy,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		now:       time.Now,
		newCode:   GenerateCode,
	}
	if err := manager.pruneExpiredLocked(); err != nil {
		return nil, err
	}
	return manager, nil
}

// SetLogger updates the logger used for access reload warnings and errors.
func (m *Manager) SetLogger(logger *slog.Logger) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	m.logger = logger
}

// --- Decisions ---

// ReloadPolicy reloads the config-backed policy and persisted access state.
func (m *Manager) ReloadPolicy(ctx context.Context) error {
	return m.reloadPolicy(ctx, ReloadSourceConfig, false)
}

// StartAutoReload starts a background watcher that reloads policy and state on disk changes.
func (m *Manager) StartAutoReload(ctx context.Context, configPath string, auditor *audit.Logger) error {
	configPath = filepath.Clean(configPath)
	if err := ctx.Err(); err != nil {
		m.logger.Debug(
			"access auto-reload not started because context is already canceled",
			"component", "access",
			"config_path", configPath,
			"state_path", m.statePath,
		)
		return nil
	}

	watcher, err := NewWatcher(configPath, m.statePath)
	if err != nil {
		return err
	}

	m.mu.Lock()
	if m.watcher != nil {
		m.mu.Unlock()
		_ = watcher.Close()
		return errors.New("access auto-reload is already running")
	}
	m.configPath = configPath
	m.auditor = auditor
	m.watcher = watcher
	m.mu.Unlock()

	go m.runAutoReload(ctx, watcher)
	return nil
}

// Evaluate returns the access decision for a message context.
func (m *Manager) Evaluate(ctx MessageContext) (Decision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.pruneExpiredLocked(); err != nil {
		return Decision{}, err
	}

	if ctx.IsDM {
		return m.evaluateDMLocked(ctx)
	}
	return m.evaluateGuildLocked(ctx), nil
}

// Approve consumes a pending pairing code.
func (m *Manager) Approve(code string) (PendingPair, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.pruneExpiredLocked(); err != nil {
		return PendingPair{}, err
	}

	pending, ok := m.state.PendingPairs[code]
	if !ok {
		return PendingPair{}, fmt.Errorf("pairing code is invalid or expired")
	}
	delete(m.state.PendingPairs, code)
	m.state.ApprovedUsers[pending.SenderID] = ApprovedUser{
		ApprovedAt: m.now().UTC(),
		Source:     "pairing",
	}
	if err := SaveState(m.statePath, m.state); err != nil {
		return PendingPair{}, err
	}
	return pending, nil
}

// Deny removes a pending pairing code.
func (m *Manager) Deny(code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.pruneExpiredLocked(); err != nil {
		return err
	}

	if _, ok := m.state.PendingPairs[code]; !ok {
		return fmt.Errorf("pairing code is invalid or expired")
	}
	delete(m.state.PendingPairs, code)
	return SaveState(m.statePath, m.state)
}

// PendingPairs returns the pending pairs in creation order.
func (m *Manager) PendingPairs() []PendingPair {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]PendingPair, 0, len(m.state.PendingPairs))
	for _, pending := range m.state.PendingPairs {
		result = append(result, pending)
	}
	slices.SortFunc(result, func(left PendingPair, right PendingPair) int {
		switch {
		case left.CreatedAt.Before(right.CreatedAt):
			return -1
		case left.CreatedAt.After(right.CreatedAt):
			return 1
		default:
			return 0
		}
	})
	return result
}

// --- Evaluation Helpers ---

func (m *Manager) evaluateDMLocked(ctx MessageContext) (Decision, error) {
	if slices.Contains(m.policy.AllowedUserIDs, ctx.UserID) {
		return Decision{Action: ActionAllow, Reason: "allowlisted_user"}, nil
	}
	if _, ok := m.state.ApprovedUsers[ctx.UserID]; ok {
		return Decision{Action: ActionAllow, Reason: "paired_user"}, nil
	}
	if !m.policy.Pairing.Enabled {
		return Decision{Action: ActionDrop, Reason: "dm_pairing_disabled"}, nil
	}

	for code, pending := range m.state.PendingPairs {
		if pending.SenderID != ctx.UserID {
			continue
		}
		if pending.ResendCount >= m.policy.Pairing.ResendLimit {
			return Decision{Action: ActionDrop, Reason: "pairing_pending_silenced"}, nil
		}
		pending.ResendCount++
		m.state.PendingPairs[code] = pending
		if err := SaveState(m.statePath, m.state); err != nil {
			return Decision{}, err
		}
		return Decision{
			Action:      ActionPair,
			Reason:      "pairing_pending",
			PairingCode: code,
			IsResend:    true,
		}, nil
	}

	if len(m.state.PendingPairs) >= m.policy.Pairing.MaxPending {
		return Decision{Action: ActionDrop, Reason: "pairing_capacity_reached"}, nil
	}

	code, err := m.newCode()
	if err != nil {
		return Decision{}, err
	}
	now := m.now().UTC()
	m.state.PendingPairs[code] = PendingPair{
		Code:        code,
		SenderID:    ctx.UserID,
		ChatID:      ctx.ChannelID,
		CreatedAt:   now,
		ExpiresAt:   now.Add(m.policy.Pairing.CodeTTL),
		ResendCount: 1,
		Nonce:       code + ":" + ctx.UserID,
	}
	if err := SaveState(m.statePath, m.state); err != nil {
		return Decision{}, err
	}

	return Decision{
		Action:      ActionPair,
		Reason:      "pairing_required",
		PairingCode: code,
		IsResend:    false,
	}, nil
}

func (m *Manager) evaluateGuildLocked(ctx MessageContext) Decision {
	channelID := ctx.EffectiveChannelID
	if channelID == "" {
		channelID = ctx.ChannelID
	}
	if !slices.Contains(m.policy.AllowedChannelIDs, channelID) {
		return Decision{Action: ActionDrop, Reason: "channel_not_allowlisted"}
	}
	if len(m.policy.AllowedUserIDs) > 0 && !slices.Contains(m.policy.AllowedUserIDs, ctx.UserID) {
		return Decision{Action: ActionDrop, Reason: "user_not_allowlisted"}
	}
	if len(m.policy.AllowedRoleIDs) > 0 && !hasAny(ctx.RoleIDs, m.policy.AllowedRoleIDs) {
		return Decision{Action: ActionDrop, Reason: "role_not_allowlisted"}
	}
	if m.policy.RequireMention && !ctx.MentionedBot && !ctx.RepliedToBot && !slices.Contains(m.policy.NoMentionChannelIDs, channelID) {
		return Decision{Action: ActionDrop, Reason: "mention_required"}
	}
	return Decision{Action: ActionAllow, Reason: "allowlisted_channel"}
}

// --- State Helpers ---

func (m *Manager) runAutoReload(ctx context.Context, watcher *Watcher) {
	defer func() {
		_ = watcher.Close()

		m.mu.Lock()
		if m.watcher == watcher {
			m.watcher = nil
		}
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events():
			if !ok {
				return
			}
			if err := m.reloadPolicy(ctx, event.Source, true); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				m.logger.Error(
					"failed to reload access policy",
					"component", "access",
					"config_path", m.configPath,
					"state_path", m.statePath,
					"source", string(event.Source),
					"error", err,
				)
			}
		case err, ok := <-watcher.Errors():
			if !ok {
				return
			}
			m.logger.Warn(
				"access watcher warning",
				"component", "access",
				"config_path", m.configPath,
				"state_path", m.statePath,
				"error", err,
			)
		}
	}
}

func (m *Manager) reloadPolicy(ctx context.Context, source ReloadSource, auditReload bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	policy, err := loadPolicy(m.configPath, m.statePath)
	if err != nil {
		return err
	}

	state, err := LoadState(m.statePath)
	if err != nil {
		return err
	}

	m.policy = policy
	m.state = state
	if err := m.pruneExpiredLocked(); err != nil {
		return err
	}
	if auditReload {
		m.recordReloadLocked(source)
	}
	return nil
}

func (m *Manager) pruneExpiredLocked() error {
	now := m.now().UTC()
	changed := false
	for code, pending := range m.state.PendingPairs {
		if pending.ExpiresAt.After(now) {
			continue
		}
		delete(m.state.PendingPairs, code)
		changed = true
	}
	if changed {
		return SaveState(m.statePath, m.state)
	}
	return nil
}

func (m *Manager) recordReloadLocked(source ReloadSource) {
	if m.auditor == nil {
		return
	}

	if err := m.auditor.Write(audit.Record{
		Timestamp: m.now().UTC(),
		Component: "access",
		Event:     "access_policy_reloaded",
		Source:    string(source),
	}); err != nil {
		m.logger.Error(
			"failed to write access reload audit record",
			"component", "access",
			"event", "access_policy_reloaded",
			"error", err,
		)
	}
}

// --- Slice Helpers ---

func hasAny(left []string, right []string) bool {
	for _, leftValue := range left {
		if slices.Contains(right, leftValue) {
			return true
		}
	}
	return false
}

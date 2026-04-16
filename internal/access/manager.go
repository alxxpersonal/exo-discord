package access

import (
	"fmt"
	"slices"
	"sync"
	"time"
)

// --- Types ---

// Manager evaluates access policy and persists pairing state.
type Manager struct {
	mu        sync.Mutex
	state     State
	statePath string
	policy    Policy
	now       func() time.Time
	newCode   func() (string, error)
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
		now:       time.Now,
		newCode:   GenerateCode,
	}
	if err := manager.pruneExpiredLocked(); err != nil {
		return nil, err
	}
	return manager, nil
}

// --- Decisions ---

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
	m.mu.Lock()
	defer m.mu.Unlock()

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
	if m.policy.RequireMention && !ctx.MentionedBot && !ctx.RepliedToBot {
		return Decision{Action: ActionDrop, Reason: "mention_required"}
	}
	return Decision{Action: ActionAllow, Reason: "allowlisted_channel"}
}

// --- State Helpers ---

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

// --- Slice Helpers ---

func hasAny(left []string, right []string) bool {
	for _, leftValue := range left {
		if slices.Contains(right, leftValue) {
			return true
		}
	}
	return false
}

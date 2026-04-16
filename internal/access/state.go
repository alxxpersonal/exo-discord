package access

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// --- Types ---

// ApprovedUser stores an approved DM user.
type ApprovedUser struct {
	ApprovedAt time.Time `json:"approved_at"`
	Source     string    `json:"source"`
}

// PendingPair stores a pending DM pairing.
type PendingPair struct {
	Code        string    `json:"code"`
	SenderID    string    `json:"sender_id"`
	ChatID      string    `json:"chat_id"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	ResendCount int       `json:"resend_count"`
	Nonce       string    `json:"nonce"`
}

// State stores approved users and pending pairs.
type State struct {
	ApprovedUsers map[string]ApprovedUser `json:"approved_users"`
	PendingPairs  map[string]PendingPair  `json:"pending_pairs"`
}

// --- State Helpers ---

// DefaultState returns an empty access state.
func DefaultState() State {
	return State{
		ApprovedUsers: map[string]ApprovedUser{},
		PendingPairs:  map[string]PendingPair{},
	}
}

// LoadState returns the saved access state for a path.
func LoadState(path string) (State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultState(), nil
		}
		return State{}, fmt.Errorf("failed to read access state %s: %w", path, err)
	}

	if err := requireExactFilePerms(path, 0o600); err != nil {
		return State{}, err
	}

	state := DefaultState()
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("failed to decode access state %s: %w", path, err)
	}
	if state.ApprovedUsers == nil {
		state.ApprovedUsers = map[string]ApprovedUser{}
	}
	if state.PendingPairs == nil {
		state.PendingPairs = map[string]PendingPair{}
	}
	return state, nil
}

// SaveState writes the access state to disk.
func SaveState(path string, state State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create access state dir %s: %w", filepath.Dir(path), err)
	}
	//nolint:gosec
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to set access state dir permissions on %s: %w", filepath.Dir(path), err)
	}

	payload, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode access state %s: %w", path, err)
	}
	payload = append(payload, '\n')

	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write access state temp file %s: %w", tempPath, err)
	}
	if err := os.Chmod(tempPath, 0o600); err != nil {
		return fmt.Errorf("failed to set access state temp permissions on %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("failed to replace access state %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to set access state permissions on %s: %w", path, err)
	}

	return nil
}

// --- Permission Helpers ---

func requireExactFilePerms(path string, want os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %s is not a regular file", path)
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("path %s must have %04o permissions, got %04o", path, want, info.Mode().Perm())
	}
	return nil
}

package userinstall

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

// --- Errors ---

// ErrTokenNotFound reports that no token exists for the requested user id.
var ErrTokenNotFound = errors.New("oauth token not found")

// --- Types ---

// StoredToken stores an oauth grant on disk.
type StoredToken struct {
	UserID       string    `json:"user_id"`
	Username     string    `json:"username,omitempty"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	TokenType    string    `json:"token_type"`
	Scope        string    `json:"scope"`
	ExpiresAt    time.Time `json:"expires_at"`
	ObtainedAt   time.Time `json:"obtained_at"`
}

// Storage persists user oauth tokens under a base directory.
type Storage struct {
	baseDir string
}

// --- Validation ---

var userIDPattern = regexp.MustCompile(`^[0-9]{1,32}$`)

// --- Constructors ---

// NewStorage creates a Storage rooted at baseDir. The directory is created with 0o700.
func NewStorage(baseDir string) (*Storage, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("oauth storage base dir must not be empty")
	}
	if err := os.MkdirAll(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("create oauth dir %s: %w", baseDir, err)
	}
	if err := os.Chmod(baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("set oauth dir perms %s: %w", baseDir, err)
	}
	return &Storage{baseDir: baseDir}, nil
}

// --- Path Helpers ---

// BaseDir returns the storage directory path.
func (s *Storage) BaseDir() string {
	return s.baseDir
}

// PathFor returns the on-disk path for a user id.
func (s *Storage) PathFor(userID string) (string, error) {
	if !userIDPattern.MatchString(userID) {
		return "", fmt.Errorf("invalid user id %q", userID)
	}
	return filepath.Join(s.baseDir, userID+".json"), nil
}

// --- Persistence ---

// Save writes a token to disk with 0o600 perms.
func (s *Storage) Save(token StoredToken) error {
	path, err := s.PathFor(token.UserID)
	if err != nil {
		return err
	}

	payload, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("encode oauth token: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("write oauth token %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("set oauth token perms %s: %w", path, err)
	}
	return nil
}

// Load reads a token from disk by user id.
func (s *Storage) Load(userID string) (StoredToken, error) {
	path, err := s.PathFor(userID)
	if err != nil {
		return StoredToken{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StoredToken{}, ErrTokenNotFound
		}
		return StoredToken{}, fmt.Errorf("read oauth token %s: %w", path, err)
	}

	var token StoredToken
	if err := json.Unmarshal(data, &token); err != nil {
		return StoredToken{}, fmt.Errorf("decode oauth token %s: %w", path, err)
	}
	return token, nil
}

// Delete removes a stored token by user id.
func (s *Storage) Delete(userID string) error {
	path, err := s.PathFor(userID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrTokenNotFound
		}
		return fmt.Errorf("delete oauth token %s: %w", path, err)
	}
	return nil
}

// List returns sorted user ids that currently have stored tokens.
func (s *Storage) List() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("list oauth dir %s: %w", s.baseDir, err)
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".json" {
			continue
		}
		base := name[:len(name)-len(".json")]
		if !userIDPattern.MatchString(base) {
			continue
		}
		ids = append(ids, base)
	}
	sort.Strings(ids)
	return ids, nil
}

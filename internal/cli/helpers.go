package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/config"
	toml "github.com/pelletier/go-toml/v2"
)

// --- JSON Helpers ---

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func saveConfig(path string, cfg config.Config) error {
	cfg.AllowedUserIDs = compactValues(cfg.AllowedUserIDs)
	cfg.AllowedChannelIDs = compactValues(cfg.AllowedChannelIDs)
	cfg.AllowedRoleIDs = compactValues(cfg.AllowedRoleIDs)
	cfg.Hook.Stdio.Command = compactValues(cfg.Hook.Stdio.Command)
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config %s: %w", path, err)
	}

	payload, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config %s: %w", path, err)
	}
	payload = append(payload, '\n')

	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write config %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to set config permissions on %s: %w", path, err)
	}
	return nil
}

// --- Access Helpers ---

func addUnique(values []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return values
	}
	if slices.Contains(values, trimmed) {
		return values
	}
	return append(values, trimmed)
}

func removeValue(values []string, value string) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return values
	}

	filtered := values[:0]
	for _, current := range values {
		if current == trimmed {
			continue
		}
		filtered = append(filtered, current)
	}
	return filtered
}

// --- Config Helpers ---

func accessPolicyFromConfig(cfg config.Config) access.Policy {
	return access.Policy{
		AllowedUserIDs:      append([]string(nil), cfg.AllowedUserIDs...),
		AllowedChannelIDs:   append([]string(nil), cfg.AllowedChannelIDs...),
		AllowedRoleIDs:      append([]string(nil), cfg.AllowedRoleIDs...),
		NoMentionChannelIDs: append([]string(nil), cfg.NoMentionChannelIDs...),
		RequireMention:      cfg.RequireMention,
		Pairing: access.PairingPolicy{
			Enabled:     cfg.Pairing.Enabled,
			CodeTTL:     cfg.Pairing.CodeTTL.Duration(),
			MaxPending:  cfg.Pairing.MaxPending,
			ResendLimit: cfg.Pairing.ResendLimit,
		},
	}
}

// --- Types ---

type approvedUserRecord struct {
	UserID     string `json:"user_id"`
	ApprovedAt string `json:"approved_at"`
	Source     string `json:"source"`
}

// --- Formatting Helpers ---

func sortedApprovedUsers(state access.State) []approvedUserRecord {
	userIDs := make([]string, 0, len(state.ApprovedUsers))
	for userID := range state.ApprovedUsers {
		userIDs = append(userIDs, userID)
	}
	sort.Strings(userIDs)

	result := make([]approvedUserRecord, 0, len(userIDs))
	for _, userID := range userIDs {
		approved := state.ApprovedUsers[userID]
		result = append(result, approvedUserRecord{
			UserID:     userID,
			ApprovedAt: approved.ApprovedAt.UTC().Format(timeFormatRFC3339),
			Source:     approved.Source,
		})
	}
	return result
}

// --- Constants ---

const timeFormatRFC3339 = "2006-01-02T15:04:05Z07:00"

package access

import (
	"fmt"

	"github.com/alxxpersonal/exo-discord/internal/config"
)

// --- Load Helpers ---

func loadPolicy(configPath string, statePath string) (Policy, error) {
	resolved, err := config.LoadResolvedPath(configPath, config.HomeDirFromAccessStatePath(statePath))
	if err != nil {
		return Policy{}, fmt.Errorf("load config %s: %w", configPath, err)
	}

	return Policy{
		AllowedUserIDs:    append([]string(nil), resolved.Config.AllowedUserIDs...),
		AllowedChannelIDs: append([]string(nil), resolved.Config.AllowedChannelIDs...),
		AllowedRoleIDs:    append([]string(nil), resolved.Config.AllowedRoleIDs...),
		RequireMention:    resolved.Config.RequireMention,
		Pairing: PairingPolicy{
			Enabled:     resolved.Config.Pairing.Enabled,
			CodeTTL:     resolved.Config.Pairing.CodeTTL.Duration(),
			MaxPending:  resolved.Config.Pairing.MaxPending,
			ResendLimit: resolved.Config.Pairing.ResendLimit,
		},
	}, nil
}

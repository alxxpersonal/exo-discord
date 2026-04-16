package cli

import (
	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/spf13/cobra"
)

// --- Types ---

type accessSummary struct {
	ConfigPath        string               `json:"config_path"`
	RequireMention    bool                 `json:"require_mention"`
	PairingEnabled    bool                 `json:"pairing_enabled"`
	AllowedUserIDs    []string             `json:"allowed_user_ids"`
	AllowedChannelIDs []string             `json:"allowed_channel_ids"`
	AllowedRoleIDs    []string             `json:"allowed_role_ids"`
	PairedUsers       []approvedUserRecord `json:"paired_users"`
	PendingPairCount  int                  `json:"pending_pair_count"`
}

type accessMutationResult struct {
	OK                bool     `json:"ok"`
	ConfigPath        string   `json:"config_path"`
	AllowedUserIDs    []string `json:"allowed_user_ids,omitempty"`
	AllowedChannelIDs []string `json:"allowed_channel_ids,omitempty"`
}

type pairedUsersResult struct {
	PairedUsers []approvedUserRecord `json:"paired_users"`
}

// --- Access Commands ---

func newAccessCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "access",
		Short:   "Inspect and update local access policy",
		Example: "exo-discord access allow-user 221773638772129792\nexo-discord access allow-channel 846209781206941736",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			state, err := access.LoadState(resolved.AccessStatePath)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), accessSummary{
				ConfigPath:        resolved.ConfigPath,
				RequireMention:    resolved.Config.RequireMention,
				PairingEnabled:    resolved.Config.Pairing.Enabled,
				AllowedUserIDs:    append([]string(nil), resolved.Config.AllowedUserIDs...),
				AllowedChannelIDs: append([]string(nil), resolved.Config.AllowedChannelIDs...),
				AllowedRoleIDs:    append([]string(nil), resolved.Config.AllowedRoleIDs...),
				PairedUsers:       sortedApprovedUsers(state),
				PendingPairCount:  len(state.PendingPairs),
			})
		},
	}

	cmd.AddCommand(newAccessAllowUserCommand(env))
	cmd.AddCommand(newAccessRemoveUserCommand(env))
	cmd.AddCommand(newAccessAllowChannelCommand(env))
	cmd.AddCommand(newAccessRemoveChannelCommand(env))
	cmd.AddCommand(newAccessListPairedCommand(env))

	return cmd
}

func newAccessAllowUserCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "allow-user <user-id>",
		Short: "Allow a Discord user id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			cfg := resolved.Config
			cfg.AllowedUserIDs = addUnique(cfg.AllowedUserIDs, args[0])
			if err := saveConfig(resolved.ConfigPath, cfg); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), accessMutationResult{
				OK:             true,
				ConfigPath:     resolved.ConfigPath,
				AllowedUserIDs: cfg.AllowedUserIDs,
			})
		},
	}
}

func newAccessRemoveUserCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-user <user-id>",
		Short: "Remove a Discord user id from the allowlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			cfg := resolved.Config
			cfg.AllowedUserIDs = removeValue(append([]string(nil), cfg.AllowedUserIDs...), args[0])
			if err := saveConfig(resolved.ConfigPath, cfg); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), accessMutationResult{
				OK:             true,
				ConfigPath:     resolved.ConfigPath,
				AllowedUserIDs: cfg.AllowedUserIDs,
			})
		},
	}
}

func newAccessAllowChannelCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "allow-channel <channel-id>",
		Short: "Allow a Discord channel id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			cfg := resolved.Config
			cfg.AllowedChannelIDs = addUnique(cfg.AllowedChannelIDs, args[0])
			if err := saveConfig(resolved.ConfigPath, cfg); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), accessMutationResult{
				OK:                true,
				ConfigPath:        resolved.ConfigPath,
				AllowedChannelIDs: cfg.AllowedChannelIDs,
			})
		},
	}
}

func newAccessRemoveChannelCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "remove-channel <channel-id>",
		Short: "Remove a Discord channel id from the allowlist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			cfg := resolved.Config
			cfg.AllowedChannelIDs = removeValue(append([]string(nil), cfg.AllowedChannelIDs...), args[0])
			if err := saveConfig(resolved.ConfigPath, cfg); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), accessMutationResult{
				OK:                true,
				ConfigPath:        resolved.ConfigPath,
				AllowedChannelIDs: cfg.AllowedChannelIDs,
			})
		},
	}
}

func newAccessListPairedCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "list-paired",
		Short: "List paired users from local access state",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			state, err := access.LoadState(resolved.AccessStatePath)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), pairedUsersResult{
				PairedUsers: sortedApprovedUsers(state),
			})
		},
	}
}

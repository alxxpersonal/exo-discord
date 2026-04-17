package managecmd

import (
	"context"
	"fmt"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Guild Commands ---

func newGuildCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "guild <id> <command>",
		Short: "Manage a specific guild",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			guildID := args[0]
			action := args[1]

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				switch action {
				case "list-channels":
					channels, err := manager.ListChannels(ctx, guildID)
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), channels)
				case "list-roles":
					roles, err := manager.ListRoles(ctx, guildID)
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), roles)
				case "list-members":
					members, err := manager.ListMembers(ctx, discord.ListMembersRequest{GuildID: guildID})
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), members)
				case "inspect":
					info, err := manager.GetGuild(ctx, guildID)
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), info)
				default:
					return fmt.Errorf("unsupported guild command %q", action)
				}
			})
		},
	}
}

package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Member Commands ---

func newMemberCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "member",
		Short: "Manage guild members",
	}

	cmd.AddCommand(newMemberListCommand(env))
	cmd.AddCommand(newMemberGetCommand(env))
	cmd.AddCommand(newMemberKickCommand(env))
	cmd.AddCommand(newMemberBanCommand(env))

	return cmd
}

func newMemberListCommand(env Environment) *cobra.Command {
	var guildID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List guild members",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				members, err := manager.ListMembers(ctx, discord.ListMembersRequest{
					GuildID: guildID,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), members)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	markRequired(cmd, "guild")
	return cmd
}

func newMemberGetCommand(env Environment) *cobra.Command {
	var guildID string

	cmd := &cobra.Command{
		Use:   "get <user-id>",
		Short: "Fetch one guild member",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				member, err := manager.GetMember(ctx, discord.GetMemberRequest{
					GuildID: guildID,
					UserID:  args[0],
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), member)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	markRequired(cmd, "guild")
	return cmd
}

func newMemberKickCommand(env Environment) *cobra.Command {
	var (
		guildID string
		yes     bool
	)

	cmd := &cobra.Command{
		Use:   "kick <user-id>",
		Short: "Kick a member from a guild",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "member kick"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.KickMember(ctx, discord.GuildUserRequest{
					GuildID: guildID,
					UserID:  args[0],
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":       true,
					"guild_id": guildID,
					"user_id":  args[0],
				})
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm member kick")
	markRequired(cmd, "guild")
	return cmd
}

func newMemberBanCommand(env Environment) *cobra.Command {
	var (
		guildID string
		yes     bool
	)

	cmd := &cobra.Command{
		Use:   "ban <user-id>",
		Short: "Ban a member from a guild",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "member ban"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.BanMember(ctx, discord.GuildUserRequest{
					GuildID: guildID,
					UserID:  args[0],
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":       true,
					"guild_id": guildID,
					"user_id":  args[0],
				})
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm member ban")
	markRequired(cmd, "guild")
	return cmd
}

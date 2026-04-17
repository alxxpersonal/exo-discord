package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/manage/colors"
	"github.com/spf13/cobra"
)

// --- Role Commands ---

func newRoleCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "role",
		Short: "Manage Discord roles",
	}

	cmd.AddCommand(newRoleListCommand(env))
	cmd.AddCommand(newRoleCreateCommand(env))
	cmd.AddCommand(newRoleUpdateCommand(env))
	cmd.AddCommand(newRoleDeleteCommand(env))
	cmd.AddCommand(newRoleAssignCommand(env))
	cmd.AddCommand(newRoleUnassignCommand(env))

	return cmd
}

func newRoleListCommand(env Environment) *cobra.Command {
	var guildID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List guild roles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				roles, err := manager.ListRoles(ctx, guildID)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), roles)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	markRequired(cmd, "guild")
	return cmd
}

func newRoleCreateCommand(env Environment) *cobra.Command {
	var (
		guildID     string
		name        string
		color       string
		hoist       bool
		mentionable bool
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a role",
		RunE: func(cmd *cobra.Command, args []string) error {
			colorValue, err := parseHexColor(color)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				role, err := manager.CreateRole(ctx, discord.RoleCreateRequest{
					GuildID:     guildID,
					Name:        name,
					Color:       colorValue,
					Hoist:       boolPointer(hoist),
					Mentionable: boolPointer(mentionable),
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), role)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().StringVar(&name, "name", "", "role name")
	cmd.Flags().StringVar(&color, "color", "", "role color in #RRGGBB format")
	cmd.Flags().BoolVar(&hoist, "hoist", false, "display users separately")
	cmd.Flags().BoolVar(&mentionable, "mentionable", false, "allow role mentions")
	markRequired(cmd, "guild")
	markRequired(cmd, "name")
	return cmd
}

func newRoleUpdateCommand(env Environment) *cobra.Command {
	var (
		guildID string
		name    string
		color   string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			colorValue, err := parseHexColor(color)
			if err != nil {
				return err
			}

			req := discord.RoleUpdateRequest{
				GuildID: guildID,
				RoleID:  args[0],
				Color:   colorValue,
			}
			if cmd.Flags().Changed("name") {
				req.Name = stringPointer(name)
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				role, err := manager.UpdateRole(ctx, req)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), role)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().StringVar(&name, "name", "", "replacement role name")
	cmd.Flags().StringVar(&color, "color", "", "replacement role color in #RRGGBB format")
	markRequired(cmd, "guild")
	return cmd
}

func newRoleDeleteCommand(env Environment) *cobra.Command {
	var (
		guildID string
		yes     bool
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a role",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "role delete"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.DeleteRole(ctx, discord.RoleDeleteRequest{GuildID: guildID, RoleID: args[0]}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"guild_id": guildID,
					"ok":       true,
					"role_id":  args[0],
				})
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm role deletion")
	markRequired(cmd, "guild")
	return cmd
}

func newRoleAssignCommand(env Environment) *cobra.Command {
	var (
		guildID string
		userID  string
		roleID  string
	)

	cmd := &cobra.Command{
		Use:   "assign",
		Short: "Assign a role to a member",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.AssignRole(ctx, discord.RoleAssignmentRequest{
					GuildID: guildID,
					UserID:  userID,
					RoleID:  roleID,
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"guild_id": guildID,
					"ok":       true,
					"user_id":  userID,
					"role_id":  roleID,
				})
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().StringVar(&userID, "user", "", "discord user id")
	cmd.Flags().StringVar(&roleID, "role", "", "discord role id")
	markRequired(cmd, "guild")
	markRequired(cmd, "user")
	markRequired(cmd, "role")
	return cmd
}

func newRoleUnassignCommand(env Environment) *cobra.Command {
	var (
		guildID string
		userID  string
		roleID  string
	)

	cmd := &cobra.Command{
		Use:   "unassign",
		Short: "Remove a role from a member",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.UnassignRole(ctx, discord.RoleAssignmentRequest{
					GuildID: guildID,
					UserID:  userID,
					RoleID:  roleID,
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"guild_id": guildID,
					"ok":       true,
					"user_id":  userID,
					"role_id":  roleID,
				})
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().StringVar(&userID, "user", "", "discord user id")
	cmd.Flags().StringVar(&roleID, "role", "", "discord role id")
	markRequired(cmd, "guild")
	markRequired(cmd, "user")
	markRequired(cmd, "role")
	return cmd
}

func parseHexColor(value string) (*int, error) {
	return colors.ParseOptionalHexColor(value)
}

func boolPointer(value bool) *bool {
	return &value
}

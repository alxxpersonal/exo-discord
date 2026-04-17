package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Channel Commands ---

func newChannelCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "channel",
		Short: "Manage Discord channels",
	}

	cmd.AddCommand(newChannelListCommand(env))
	cmd.AddCommand(newChannelCreateCommand(env))
	cmd.AddCommand(newChannelUpdateCommand(env))
	cmd.AddCommand(newChannelDeleteCommand(env))
	cmd.AddCommand(newChannelPermSetCommand(env))
	cmd.AddCommand(newChannelPermRemoveCommand(env))

	return cmd
}

func newChannelListCommand(env Environment) *cobra.Command {
	var guildID string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List channels in a guild",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				channels, err := manager.ListChannels(ctx, guildID)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), channels)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	markRequired(cmd, "guild")
	return cmd
}

func newChannelCreateCommand(env Environment) *cobra.Command {
	var (
		guildID     string
		name        string
		channelType string
		parentID    string
		topic       string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a channel",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				channel, err := manager.CreateChannel(ctx, discord.ChannelCreateRequest{
					GuildID:  guildID,
					Name:     name,
					Type:     channelType,
					ParentID: parentID,
					Topic:    topic,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), channel)
			})
		},
	}

	cmd.Flags().StringVar(&guildID, "guild", "", "discord guild id")
	cmd.Flags().StringVar(&name, "name", "", "channel name")
	cmd.Flags().StringVar(&channelType, "type", "text", "channel type: text, voice, category")
	cmd.Flags().StringVar(&parentID, "parent", "", "optional parent category channel id")
	cmd.Flags().StringVar(&topic, "topic", "", "optional channel topic")
	markRequired(cmd, "guild")
	markRequired(cmd, "name")
	return cmd
}

func newChannelUpdateCommand(env Environment) *cobra.Command {
	var (
		name     string
		topic    string
		parentID string
	)

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var req discord.ChannelUpdateRequest
			req.ID = args[0]
			if cmd.Flags().Changed("name") {
				req.Name = stringPointer(name)
			}
			if cmd.Flags().Changed("topic") {
				req.Topic = stringPointer(topic)
			}
			if cmd.Flags().Changed("parent") {
				req.ParentID = stringPointer(parentID)
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				channel, err := manager.UpdateChannel(ctx, req)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), channel)
			})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "replacement channel name")
	cmd.Flags().StringVar(&topic, "topic", "", "replacement channel topic")
	cmd.Flags().StringVar(&parentID, "parent", "", "replacement parent category id")
	return cmd
}

func newChannelDeleteCommand(env Environment) *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "channel delete"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.DeleteChannel(ctx, args[0]); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":         true,
					"channel_id": args[0],
				})
			})
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "confirm channel deletion")
	return cmd
}

func newChannelPermSetCommand(env Environment) *cobra.Command {
	var (
		roleID string
		allow  []string
		deny   []string
	)

	cmd := &cobra.Command{
		Use:   "perm-set <id>",
		Short: "Create or update a role overwrite on a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.SetChannelPermission(ctx, discord.ChannelPermissionSetRequest{
					ChannelID:  args[0],
					TargetID:   roleID,
					TargetType: "role",
					Allow:      allow,
					Deny:       deny,
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":         true,
					"channel_id": args[0],
					"role_id":    roleID,
				})
			})
		},
	}

	cmd.Flags().StringVar(&roleID, "role", "", "discord role id")
	cmd.Flags().StringSliceVar(&allow, "allow", nil, "comma-separated permissions to allow")
	cmd.Flags().StringSliceVar(&deny, "deny", nil, "comma-separated permissions to deny")
	markRequired(cmd, "role")
	return cmd
}

func newChannelPermRemoveCommand(env Environment) *cobra.Command {
	var roleID string

	cmd := &cobra.Command{
		Use:   "perm-remove <id>",
		Short: "Delete a role overwrite from a channel",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.RemoveChannelPermission(ctx, discord.ChannelPermissionRemoveRequest{
					ChannelID:  args[0],
					TargetID:   roleID,
					TargetType: "role",
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":         true,
					"channel_id": args[0],
					"role_id":    roleID,
				})
			})
		},
	}

	cmd.Flags().StringVar(&roleID, "role", "", "discord role id")
	markRequired(cmd, "role")
	return cmd
}

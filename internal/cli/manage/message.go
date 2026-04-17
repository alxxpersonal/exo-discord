package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Message Commands ---

func newMessageCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "message",
		Short: "Manage Discord messages",
	}

	cmd.AddCommand(newMessageSendCommand(env))
	cmd.AddCommand(newMessageEditCommand(env))
	cmd.AddCommand(newMessageDeleteCommand(env))
	cmd.AddCommand(newMessageBulkDeleteCommand(env))
	cmd.AddCommand(newMessageReactCommand(env))

	return cmd
}

func newMessageSendCommand(env Environment) *cobra.Command {
	var (
		channelID string
		text      string
		files     []string
	)

	cmd := &cobra.Command{
		Use:   "send",
		Short: "Send a message",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				message, err := manager.SendManagedMessage(ctx, discord.SendRequest{
					ChannelID: channelID,
					Text:      text,
					Files:     files,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), message)
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&text, "text", "", "message text")
	cmd.Flags().StringSliceVar(&files, "file", nil, "absolute file paths to attach")
	markRequired(cmd, "channel")
	markRequired(cmd, "text")
	return cmd
}

func newMessageEditCommand(env Environment) *cobra.Command {
	var (
		channelID string
		text      string
	)

	cmd := &cobra.Command{
		Use:   "edit <id>",
		Short: "Edit a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				message, err := manager.EditManagedMessage(ctx, discord.EditRequest{
					ChannelID: channelID,
					MessageID: args[0],
					Text:      text,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), message)
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&text, "text", "", "replacement text")
	markRequired(cmd, "channel")
	markRequired(cmd, "text")
	return cmd
}

func newMessageDeleteCommand(env Environment) *cobra.Command {
	var (
		channelID string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "message delete"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.DeleteManagedMessage(ctx, discord.MessageTarget{
					ChannelID: channelID,
					MessageID: args[0],
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":         true,
					"channel_id": channelID,
					"message_id": args[0],
				})
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm message deletion")
	markRequired(cmd, "channel")
	return cmd
}

func newMessageBulkDeleteCommand(env Environment) *cobra.Command {
	var (
		channelID string
		userID    string
		beforeID  string
		yes       bool
	)

	cmd := &cobra.Command{
		Use:   "bulk-delete",
		Short: "Bulk delete messages by author in a channel",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireYes(yes, "message bulk-delete"); err != nil {
				return err
			}
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				result, err := manager.BulkDeleteMessages(ctx, discord.BulkDeleteRequest{
					ChannelID: channelID,
					UserID:    userID,
					BeforeID:  beforeID,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), result)
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&userID, "user", "", "discord user id")
	cmd.Flags().StringVar(&beforeID, "before", "", "delete messages before this message id")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm bulk deletion")
	markRequired(cmd, "channel")
	markRequired(cmd, "user")
	return cmd
}

func newMessageReactCommand(env Environment) *cobra.Command {
	var (
		channelID string
		emoji     string
	)

	cmd := &cobra.Command{
		Use:   "react <id>",
		Short: "React to a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := manager.ReactToManagedMessage(ctx, discord.ReactRequest{
					ChannelID: channelID,
					MessageID: args[0],
					Emoji:     emoji,
				}); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":         true,
					"channel_id": channelID,
					"message_id": args[0],
					"emoji":      emoji,
				})
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&emoji, "emoji", "", "emoji to add")
	markRequired(cmd, "channel")
	markRequired(cmd, "emoji")
	return cmd
}

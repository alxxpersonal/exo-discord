package cli

import (
	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Types ---

type sendResult struct {
	OK        bool   `json:"ok"`
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Emoji     string `json:"emoji,omitempty"`
}

// --- Send Commands ---

func newSendCommand(env Environment) *cobra.Command {
	var (
		channelID string
		text      string
		files     []string
	)

	cmd := &cobra.Command{
		Use:     "send",
		Short:   "Send a message to Discord",
		Example: "exo-discord send --channel 846209781206941736 --text \"hello from exo-discord\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			sent, err := session.SendMessage(env.commandContext(), discord.SendRequest{
				ChannelID: channelID,
				Text:      text,
				Files:     append([]string(nil), files...),
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sendResult{
				OK:        true,
				ChannelID: sent.ChannelID,
				MessageID: sent.MessageID,
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&text, "text", "", "message text")
	cmd.Flags().StringSliceVar(&files, "file", nil, "absolute file paths to attach")
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("text")

	cmd.AddCommand(newSendReplyCommand(env))
	cmd.AddCommand(newSendEditCommand(env))
	cmd.AddCommand(newSendReactCommand(env))

	return cmd
}

func newSendReplyCommand(env Environment) *cobra.Command {
	var (
		channelID string
		guildID   string
		messageID string
		text      string
		files     []string
	)

	cmd := &cobra.Command{
		Use:     "reply",
		Short:   "Reply to a Discord message",
		Example: "exo-discord send reply --channel 846209781206941736 --message 1352054123456789012 --text \"on it\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			sent, err := session.Reply(env.commandContext(), discord.ReplyRequest{
				ChannelID:        channelID,
				GuildID:          guildID,
				Text:             text,
				ReplyToMessageID: messageID,
				Files:            append([]string(nil), files...),
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sendResult{
				OK:        true,
				ChannelID: sent.ChannelID,
				MessageID: sent.MessageID,
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&guildID, "guild", "", "optional guild id")
	cmd.Flags().StringVar(&messageID, "message", "", "target message id")
	cmd.Flags().StringVar(&text, "text", "", "reply text")
	cmd.Flags().StringSliceVar(&files, "file", nil, "absolute file paths to attach")
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("message")
	_ = cmd.MarkFlagRequired("text")

	return cmd
}

func newSendEditCommand(env Environment) *cobra.Command {
	var (
		channelID string
		messageID string
		text      string
	)

	cmd := &cobra.Command{
		Use:     "edit",
		Short:   "Edit a bot-authored Discord message",
		Example: "exo-discord send edit --channel 846209781206941736 --message 1352054123456789012 --text \"updated\"",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			sent, err := session.EditMessage(env.commandContext(), discord.EditRequest{
				ChannelID: channelID,
				MessageID: messageID,
				Text:      text,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sendResult{
				OK:        true,
				ChannelID: sent.ChannelID,
				MessageID: sent.MessageID,
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&messageID, "message", "", "target message id")
	cmd.Flags().StringVar(&text, "text", "", "replacement text")
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("message")
	_ = cmd.MarkFlagRequired("text")

	return cmd
}

func newSendReactCommand(env Environment) *cobra.Command {
	var (
		channelID string
		messageID string
		emoji     string
	)

	cmd := &cobra.Command{
		Use:     "react",
		Short:   "React to a Discord message",
		Example: "exo-discord send react --channel 846209781206941736 --message 1352054123456789012 --emoji 👍",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			if err := session.React(env.commandContext(), discord.ReactRequest{
				ChannelID: channelID,
				MessageID: messageID,
				Emoji:     emoji,
			}); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), sendResult{
				OK:        true,
				ChannelID: channelID,
				MessageID: messageID,
				Emoji:     emoji,
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&messageID, "message", "", "target message id")
	cmd.Flags().StringVar(&emoji, "emoji", "", "emoji to add")
	_ = cmd.MarkFlagRequired("channel")
	_ = cmd.MarkFlagRequired("message")
	_ = cmd.MarkFlagRequired("emoji")

	return cmd
}

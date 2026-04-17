package managecmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Embed Commands ---

func newEmbedCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "embed",
		Short: "Manage embeds and components",
	}

	cmd.AddCommand(newEmbedPostCommand(env))
	cmd.AddCommand(newEmbedAddButtonCommand(env))
	cmd.AddCommand(newEmbedAddSelectCommand(env))

	return cmd
}

func newEmbedPostCommand(env Environment) *cobra.Command {
	var (
		channelID string
		title     string
		desc      string
		color     string
		image     string
		fields    []string
	)

	cmd := &cobra.Command{
		Use:   "post",
		Short: "Post an embed",
		RunE: func(cmd *cobra.Command, args []string) error {
			colorValue, err := parseHexColor(color)
			if err != nil {
				return err
			}
			embedFields, err := parseEmbedFields(fields)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				message, err := manager.PostEmbed(ctx, discord.EmbedPostRequest{
					ChannelID:   channelID,
					Title:       title,
					Description: desc,
					Color:       colorValue,
					Image:       image,
					Fields:      embedFields,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), message)
			})
		},
	}

	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&title, "title", "", "embed title")
	cmd.Flags().StringVar(&desc, "desc", "", "embed description")
	cmd.Flags().StringVar(&color, "color", "", "embed color in #RRGGBB format")
	cmd.Flags().StringVar(&image, "image", "", "embed image url")
	cmd.Flags().StringSliceVar(&fields, "field", nil, "embed field in name:value format")
	markRequired(cmd, "channel")
	return cmd
}

func newEmbedAddButtonCommand(env Environment) *cobra.Command {
	var (
		messageID string
		channelID string
		label     string
		customID  string
		style     string
	)

	cmd := &cobra.Command{
		Use:   "add-button",
		Short: "Append a button to an existing message",
		RunE: func(cmd *cobra.Command, args []string) error {
			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				message, err := manager.AddButton(ctx, discord.ButtonAddRequest{
					ChannelID: channelID,
					MessageID: messageID,
					Label:     label,
					CustomID:  customID,
					Style:     style,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), message)
			})
		},
	}

	cmd.Flags().StringVar(&messageID, "message", "", "discord message id")
	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&label, "label", "", "button label")
	cmd.Flags().StringVar(&customID, "custom-id", "", "button custom id")
	cmd.Flags().StringVar(&style, "style", "primary", "button style")
	markRequired(cmd, "message")
	markRequired(cmd, "channel")
	markRequired(cmd, "label")
	markRequired(cmd, "custom-id")
	return cmd
}

func newEmbedAddSelectCommand(env Environment) *cobra.Command {
	var (
		messageID string
		channelID string
		customID  string
		options   string
	)

	cmd := &cobra.Command{
		Use:   "add-select",
		Short: "Append a select menu to an existing message",
		RunE: func(cmd *cobra.Command, args []string) error {
			parsedOptions, err := parseSelectOptions(options)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				resolvedCustomID := customID
				if resolvedCustomID == "" {
					resolvedCustomID = fmt.Sprintf("select-%s-%d", messageID, time.Now().UnixNano())
				}
				message, err := manager.AddSelect(ctx, discord.SelectAddRequest{
					ChannelID: channelID,
					MessageID: messageID,
					CustomID:  resolvedCustomID,
					Options:   parsedOptions,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), message)
			})
		},
	}

	cmd.Flags().StringVar(&messageID, "message", "", "discord message id")
	cmd.Flags().StringVar(&channelID, "channel", "", "discord channel id")
	cmd.Flags().StringVar(&customID, "custom-id", "", "select custom id")
	cmd.Flags().StringVar(&options, "options", "", "select options as label:value,label:value")
	markRequired(cmd, "message")
	markRequired(cmd, "channel")
	markRequired(cmd, "options")
	return cmd
}

func parseEmbedFields(values []string) ([]discord.EmbedField, error) {
	fields := make([]discord.EmbedField, 0, len(values))
	for _, value := range values {
		parts := strings.SplitN(value, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid field %q", value)
		}
		fields = append(fields, discord.EmbedField{
			Name:  strings.TrimSpace(parts[0]),
			Value: strings.TrimSpace(parts[1]),
		})
	}
	return fields, nil
}

func parseSelectOptions(value string) ([]discord.SelectOption, error) {
	parts := strings.Split(value, ",")
	options := make([]discord.SelectOption, 0, len(parts))
	for _, part := range parts {
		pieces := strings.SplitN(strings.TrimSpace(part), ":", 2)
		if len(pieces) != 2 {
			return nil, fmt.Errorf("invalid select option %q", part)
		}
		options = append(options, discord.SelectOption{
			Label: strings.TrimSpace(pieces[0]),
			Value: strings.TrimSpace(pieces[1]),
		})
	}
	return options, nil
}

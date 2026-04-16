package mcp

import (
	"context"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Tool Args ---

type sendMessageArgs struct {
	ChannelID string   `json:"channel_id" jsonschema:"Discord channel id"`
	Text      string   `json:"text" jsonschema:"Message text to send"`
	Files     []string `json:"files,omitempty" jsonschema:"Absolute file paths to attach"`
}

type sendMessageResult struct {
	OK        bool   `json:"ok"`
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

type replyArgs struct {
	ChannelID        string   `json:"channel_id" jsonschema:"Discord channel id"`
	GuildID          string   `json:"guild_id,omitempty" jsonschema:"Optional guild id for reply reference"`
	Text             string   `json:"text" jsonschema:"Reply text"`
	ReplyToMessageID string   `json:"reply_to_message_id" jsonschema:"Message id to reply to"`
	Files            []string `json:"files,omitempty" jsonschema:"Absolute file paths to attach"`
}

type reactArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID string `json:"message_id" jsonschema:"Target message id"`
	Emoji     string `json:"emoji" jsonschema:"Emoji to add"`
}

type editMessageArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID string `json:"message_id" jsonschema:"Message id to edit"`
	Text      string `json:"text" jsonschema:"Replacement content"`
}

type fetchHistoryArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	BeforeID  string `json:"before,omitempty" jsonschema:"Fetch messages before this id"`
	AfterID   string `json:"after,omitempty" jsonschema:"Fetch messages after this id"`
	AroundID  string `json:"around,omitempty" jsonschema:"Fetch messages around this id"`
	Limit     int    `json:"limit,omitempty" jsonschema:"Number of messages to fetch, max 100"`
}

type fetchHistoryResult struct {
	Messages []discordpkg.Message `json:"messages"`
}

type downloadAttachmentArgs struct {
	ChannelID      string `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID      string `json:"message_id" jsonschema:"Message id holding the attachment"`
	AttachmentID   string `json:"attachment_id,omitempty" jsonschema:"Optional single attachment id"`
	DestinationDir string `json:"destination_dir" jsonschema:"Absolute destination directory"`
	MaxBytes       int64  `json:"max_bytes" jsonschema:"Maximum allowed attachment size in bytes"`
}

type downloadAttachmentResult struct {
	Files []discordpkg.DownloadedFile `json:"files"`
}

type editMessageResult struct {
	OK        bool   `json:"ok"`
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

type reactResult struct {
	OK bool `json:"ok"`
}

type setStatusArgs struct {
	Presence     string `json:"presence" jsonschema:"online, idle, dnd, or invisible"`
	ActivityType string `json:"activity_type,omitempty" jsonschema:"playing, streaming, listening, watching, competing"`
	ActivityText string `json:"activity_text,omitempty" jsonschema:"Activity text"`
}

type setStatusResult struct {
	OK bool `json:"ok"`
}

// --- Tool Registration ---

func registerTools(server *sdkmcp.Server, session discordpkg.Session) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "send_message",
		Description: "Send a standalone Discord message to a channel.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sendMessageArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := session.SendMessage(ctx, discordpkg.SendRequest{
			ChannelID: args.ChannelID,
			Text:      args.Text,
			Files:     args.Files,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}

		return nil, sendMessageResult{
			OK:        true,
			ChannelID: sent.ChannelID,
			MessageID: sent.MessageID,
		}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "reply",
		Description: "Reply to a Discord message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args replyArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := session.Reply(ctx, discordpkg.ReplyRequest{
			ChannelID:        args.ChannelID,
			GuildID:          args.GuildID,
			Text:             args.Text,
			ReplyToMessageID: args.ReplyToMessageID,
			Files:            args.Files,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}

		return nil, sendMessageResult{
			OK:        true,
			ChannelID: sent.ChannelID,
			MessageID: sent.MessageID,
		}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "react",
		Description: "Add an emoji reaction to a Discord message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args reactArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := session.React(ctx, discordpkg.ReactRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			Emoji:     args.Emoji,
		}); err != nil {
			return nil, reactResult{}, err
		}

		return nil, reactResult{OK: true}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "fetch_history",
		Description: "Fetch recent Discord channel history.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args fetchHistoryArgs) (*sdkmcp.CallToolResult, fetchHistoryResult, error) {
		messages, err := session.FetchHistory(ctx, discordpkg.HistoryRequest{
			ChannelID: args.ChannelID,
			BeforeID:  args.BeforeID,
			AfterID:   args.AfterID,
			AroundID:  args.AroundID,
			Limit:     args.Limit,
		})
		if err != nil {
			return nil, fetchHistoryResult{}, err
		}

		return nil, fetchHistoryResult{Messages: messages}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "download_attachment",
		Description: "Download one or more attachments from a Discord message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args downloadAttachmentArgs) (*sdkmcp.CallToolResult, downloadAttachmentResult, error) {
		files, err := session.DownloadAttachments(ctx, discordpkg.DownloadRequest{
			ChannelID:      args.ChannelID,
			MessageID:      args.MessageID,
			AttachmentID:   args.AttachmentID,
			DestinationDir: args.DestinationDir,
			MaxBytes:       args.MaxBytes,
		})
		if err != nil {
			return nil, downloadAttachmentResult{}, err
		}

		return nil, downloadAttachmentResult{Files: files}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "edit_message",
		Description: "Edit a Discord message authored by the bot.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args editMessageArgs) (*sdkmcp.CallToolResult, editMessageResult, error) {
		sent, err := session.EditMessage(ctx, discordpkg.EditRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			Text:      args.Text,
		})
		if err != nil {
			return nil, editMessageResult{}, err
		}

		return nil, editMessageResult{
			OK:        true,
			ChannelID: sent.ChannelID,
			MessageID: sent.MessageID,
		}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "set_status",
		Description: "Set the bot presence and optional activity.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args setStatusArgs) (*sdkmcp.CallToolResult, setStatusResult, error) {
		if err := session.SetStatus(ctx, discordpkg.StatusRequest{
			Presence:     args.Presence,
			ActivityType: args.ActivityType,
			ActivityText: args.ActivityText,
		}); err != nil {
			return nil, setStatusResult{}, err
		}

		return nil, setStatusResult{OK: true}, nil
	})
}

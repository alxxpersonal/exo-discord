package mcp

import (
	"context"
	"fmt"
	"os"
	"time"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	runtimeexec "github.com/alxxpersonal/exo-discord/internal/manage/runtimeexec"
	scaffoldpkg "github.com/alxxpersonal/exo-discord/internal/manage/scaffold"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Session Tool Args ---

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

type guildChannelsResult struct {
	Channels []discordpkg.GuildChannel `json:"channels"`
}

type guildRolesResult struct {
	Roles []discordpkg.GuildRole `json:"roles"`
}

type guildMembersResult struct {
	Members []discordpkg.GuildMember `json:"members"`
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

// --- Manager Tool Args ---

type guildIDArgs struct {
	GuildID string `json:"guild_id" jsonschema:"Discord guild id"`
}

type userGuildArgs struct {
	GuildID string `json:"guild_id" jsonschema:"Discord guild id"`
	UserID  string `json:"user_id" jsonschema:"Discord user id"`
}

type channelCreateArgs struct {
	GuildID  string `json:"guild_id" jsonschema:"Discord guild id"`
	Name     string `json:"name" jsonschema:"Channel name"`
	Type     string `json:"type" jsonschema:"Channel type: text, voice, category"`
	ParentID string `json:"parent_id,omitempty" jsonschema:"Optional parent category channel id"`
	Topic    string `json:"topic,omitempty" jsonschema:"Optional channel topic"`
}

type channelUpdateArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	Name      string `json:"name,omitempty" jsonschema:"Replacement channel name"`
	Topic     string `json:"topic,omitempty" jsonschema:"Replacement topic"`
	ParentID  string `json:"parent_id,omitempty" jsonschema:"Replacement parent category id"`
}

type channelDeleteArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type channelPermissionArgs struct {
	ChannelID  string   `json:"channel_id" jsonschema:"Discord channel id"`
	TargetID   string   `json:"target_id" jsonschema:"Discord role or member id"`
	TargetType string   `json:"target_type,omitempty" jsonschema:"role or member"`
	Allow      []string `json:"allow,omitempty" jsonschema:"Permissions to allow"`
	Deny       []string `json:"deny,omitempty" jsonschema:"Permissions to deny"`
}

type roleCreateArgs struct {
	GuildID     string `json:"guild_id" jsonschema:"Discord guild id"`
	Name        string `json:"name" jsonschema:"Role name"`
	Color       *int   `json:"color,omitempty" jsonschema:"Role color as decimal int"`
	Hoist       *bool  `json:"hoist,omitempty" jsonschema:"Whether the role is hoisted"`
	Mentionable *bool  `json:"mentionable,omitempty" jsonschema:"Whether the role is mentionable"`
}

type roleUpdateArgs struct {
	GuildID     string `json:"guild_id,omitempty" jsonschema:"Optional discord guild id"`
	RoleID      string `json:"role_id" jsonschema:"Discord role id"`
	Name        string `json:"name,omitempty" jsonschema:"Replacement role name"`
	Color       *int   `json:"color,omitempty" jsonschema:"Replacement role color as decimal int"`
	Hoist       *bool  `json:"hoist,omitempty" jsonschema:"Replacement hoist value"`
	Mentionable *bool  `json:"mentionable,omitempty" jsonschema:"Replacement mentionable value"`
}

type roleDeleteArgs struct {
	GuildID string `json:"guild_id,omitempty" jsonschema:"Optional discord guild id"`
	RoleID  string `json:"role_id" jsonschema:"Discord role id"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type memberActionArgs struct {
	GuildID string `json:"guild_id" jsonschema:"Discord guild id"`
	UserID  string `json:"user_id" jsonschema:"Discord user id"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type roleAssignArgs struct {
	GuildID string `json:"guild_id,omitempty" jsonschema:"Optional discord guild id"`
	UserID  string `json:"user_id" jsonschema:"Discord user id"`
	RoleID  string `json:"role_id" jsonschema:"Discord role id"`
}

type messageDeleteArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID string `json:"message_id" jsonschema:"Discord message id"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type bulkDeleteArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	UserID    string `json:"user_id" jsonschema:"Discord user id"`
	BeforeID  string `json:"before_id,omitempty" jsonschema:"Delete messages before this message id"`
	Confirm   bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type embedPostArgs struct {
	ChannelID   string                  `json:"channel_id" jsonschema:"Discord channel id"`
	Title       string                  `json:"title,omitempty" jsonschema:"Embed title"`
	Description string                  `json:"description,omitempty" jsonschema:"Embed description"`
	Color       *int                    `json:"color,omitempty" jsonschema:"Embed color as decimal int"`
	Image       string                  `json:"image,omitempty" jsonschema:"Embed image url"`
	Fields      []discordpkg.EmbedField `json:"fields,omitempty" jsonschema:"Embed fields"`
}

type buttonAddArgs struct {
	ChannelID string `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID string `json:"message_id" jsonschema:"Discord message id"`
	Label     string `json:"label" jsonschema:"Button label"`
	CustomID  string `json:"custom_id" jsonschema:"Button custom id"`
	Style     string `json:"style,omitempty" jsonschema:"Button style"`
}

type selectAddArgs struct {
	ChannelID string                    `json:"channel_id" jsonschema:"Discord channel id"`
	MessageID string                    `json:"message_id" jsonschema:"Discord message id"`
	CustomID  string                    `json:"custom_id" jsonschema:"Select custom id"`
	Options   []discordpkg.SelectOption `json:"options" jsonschema:"Select options"`
}

type interactionRespondArgs struct {
	InteractionID    string `json:"interaction_id" jsonschema:"Discord interaction id"`
	InteractionToken string `json:"interaction_token" jsonschema:"Discord interaction token"`
	Type             int    `json:"type" jsonschema:"Discord interaction response type"`
	Body             string `json:"body,omitempty" jsonschema:"JSON interaction response data object"`
}

type interactionListenArgs struct {
	Limit          int `json:"limit,omitempty" jsonschema:"Maximum events to capture"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"How long to listen before returning"`
}

type interactionListenResult struct {
	Events []discordpkg.InteractionEvent `json:"events"`
}

type restArgs struct {
	Method string `json:"method" jsonschema:"HTTP method"`
	Path   string `json:"path" jsonschema:"Discord API path or URL"`
	Body   string `json:"body,omitempty" jsonschema:"JSON request body"`
}

type scaffoldArgs struct {
	Path         string `json:"path" jsonschema:"Path to a YAML scaffold file"`
	Apply        bool   `json:"apply,omitempty" jsonschema:"Apply the plan instead of just diffing"`
	DeleteExtras bool   `json:"delete_extras,omitempty" jsonschema:"Delete live roles and channels missing from the scaffold"`
	Confirm      bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type scaffoldResult struct {
	Applied bool             `json:"applied"`
	Plan    scaffoldpkg.Plan `json:"plan"`
}

type execArgs struct {
	Path    string `json:"path,omitempty" jsonschema:"Optional path to a Go source file"`
	Source  string `json:"source,omitempty" jsonschema:"Inline Go source"`
	Confirm bool   `json:"confirm,omitempty" jsonschema:"Set true to confirm this destructive action"`
}

type execResult struct {
	OK bool `json:"ok"`
}

const destructiveConfirmError = "destructive action requires confirm=true"

// --- Tool Registration ---

func registerTools(server *sdkmcp.Server, session discordpkg.Session, manager discordpkg.Manager) {
	registerSessionTools(server, session)
	registerManagerTools(server, manager)
}

func registerSessionTools(server *sdkmcp.Server, session discordpkg.Session) {
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

func registerManagerTools(server *sdkmcp.Server, manager discordpkg.Manager) {
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_guild_list_channels",
		Description: "List channels in a guild.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildChannelsResult, error) {
		result, err := manager.ListChannels(ctx, args.GuildID)
		return nil, guildChannelsResult{Channels: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_guild_list_roles",
		Description: "List roles in a guild.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildRolesResult, error) {
		result, err := manager.ListRoles(ctx, args.GuildID)
		return nil, guildRolesResult{Roles: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_guild_list_members",
		Description: "List members in a guild.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildMembersResult, error) {
		result, err := manager.ListMembers(ctx, discordpkg.ListMembersRequest{GuildID: args.GuildID})
		return nil, guildMembersResult{Members: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_guild_inspect",
		Description: "Inspect guild metadata.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, discordpkg.GuildInfo, error) {
		result, err := manager.GetGuild(ctx, args.GuildID)
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_list",
		Description: "List channels in a guild.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildChannelsResult, error) {
		result, err := manager.ListChannels(ctx, args.GuildID)
		return nil, guildChannelsResult{Channels: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_create",
		Description: "Create a channel.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args channelCreateArgs) (*sdkmcp.CallToolResult, discordpkg.GuildChannel, error) {
		result, err := manager.CreateChannel(ctx, discordpkg.ChannelCreateRequest{
			GuildID:  args.GuildID,
			Name:     args.Name,
			Type:     args.Type,
			ParentID: args.ParentID,
			Topic:    args.Topic,
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_update",
		Description: "Update a channel.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args channelUpdateArgs) (*sdkmcp.CallToolResult, discordpkg.GuildChannel, error) {
		result, err := manager.UpdateChannel(ctx, discordpkg.ChannelUpdateRequest{
			ID:       args.ChannelID,
			Name:     stringPointer(args.Name),
			Topic:    stringPointer(args.Topic),
			ParentID: stringPointer(args.ParentID),
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_delete",
		Description: "Delete a channel.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args channelDeleteArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, reactResult{}, err
		}
		err := manager.DeleteChannel(ctx, args.ChannelID)
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_perm_set",
		Description: "Create or update a channel permission overwrite.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args channelPermissionArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.SetChannelPermission(ctx, discordpkg.ChannelPermissionSetRequest{
			ChannelID:  args.ChannelID,
			TargetID:   args.TargetID,
			TargetType: args.TargetType,
			Allow:      args.Allow,
			Deny:       args.Deny,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_channel_perm_remove",
		Description: "Delete a channel permission overwrite.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args channelPermissionArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.RemoveChannelPermission(ctx, discordpkg.ChannelPermissionRemoveRequest{
			ChannelID:  args.ChannelID,
			TargetID:   args.TargetID,
			TargetType: args.TargetType,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_list",
		Description: "List roles in a guild.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildRolesResult, error) {
		result, err := manager.ListRoles(ctx, args.GuildID)
		return nil, guildRolesResult{Roles: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_create",
		Description: "Create a role.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args roleCreateArgs) (*sdkmcp.CallToolResult, discordpkg.GuildRole, error) {
		result, err := manager.CreateRole(ctx, discordpkg.RoleCreateRequest{
			GuildID:     args.GuildID,
			Name:        args.Name,
			Color:       args.Color,
			Hoist:       args.Hoist,
			Mentionable: args.Mentionable,
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_update",
		Description: "Update a role.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args roleUpdateArgs) (*sdkmcp.CallToolResult, discordpkg.GuildRole, error) {
		result, err := manager.UpdateRole(ctx, discordpkg.RoleUpdateRequest{
			GuildID:     args.GuildID,
			RoleID:      args.RoleID,
			Name:        stringPointer(args.Name),
			Color:       args.Color,
			Hoist:       args.Hoist,
			Mentionable: args.Mentionable,
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_delete",
		Description: "Delete a role.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args roleDeleteArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, reactResult{}, err
		}
		err := manager.DeleteRole(ctx, discordpkg.RoleDeleteRequest{
			GuildID: args.GuildID,
			RoleID:  args.RoleID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_assign",
		Description: "Assign a role to a guild member.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args roleAssignArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.AssignRole(ctx, discordpkg.RoleAssignmentRequest{
			GuildID: args.GuildID,
			UserID:  args.UserID,
			RoleID:  args.RoleID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_role_unassign",
		Description: "Remove a role from a guild member.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args roleAssignArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.UnassignRole(ctx, discordpkg.RoleAssignmentRequest{
			GuildID: args.GuildID,
			UserID:  args.UserID,
			RoleID:  args.RoleID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_member_list",
		Description: "List guild members.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args guildIDArgs) (*sdkmcp.CallToolResult, guildMembersResult, error) {
		result, err := manager.ListMembers(ctx, discordpkg.ListMembersRequest{GuildID: args.GuildID})
		return nil, guildMembersResult{Members: result}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_member_get",
		Description: "Fetch a single guild member.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args userGuildArgs) (*sdkmcp.CallToolResult, discordpkg.GuildMember, error) {
		result, err := manager.GetMember(ctx, discordpkg.GetMemberRequest{
			GuildID: args.GuildID,
			UserID:  args.UserID,
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_member_kick",
		Description: "Kick a guild member.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args memberActionArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, reactResult{}, err
		}
		err := manager.KickMember(ctx, discordpkg.GuildUserRequest{
			GuildID: args.GuildID,
			UserID:  args.UserID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_member_ban",
		Description: "Ban a guild member.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args memberActionArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, reactResult{}, err
		}
		err := manager.BanMember(ctx, discordpkg.GuildUserRequest{
			GuildID: args.GuildID,
			UserID:  args.UserID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_message_send",
		Description: "Send a Discord message through the manager surface.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sendMessageArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := manager.SendManagedMessage(ctx, discordpkg.SendRequest{
			ChannelID: args.ChannelID,
			Text:      args.Text,
			Files:     args.Files,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}
		return nil, sendMessageResult{OK: true, ChannelID: sent.ChannelID, MessageID: sent.MessageID}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_message_edit",
		Description: "Edit a Discord message through the manager surface.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args editMessageArgs) (*sdkmcp.CallToolResult, editMessageResult, error) {
		sent, err := manager.EditManagedMessage(ctx, discordpkg.EditRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			Text:      args.Text,
		})
		if err != nil {
			return nil, editMessageResult{}, err
		}
		return nil, editMessageResult{OK: true, ChannelID: sent.ChannelID, MessageID: sent.MessageID}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_message_delete",
		Description: "Delete a Discord message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args messageDeleteArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, reactResult{}, err
		}
		err := manager.DeleteManagedMessage(ctx, discordpkg.MessageTarget{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_message_bulk_delete",
		Description: "Bulk delete messages by author in a channel.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args bulkDeleteArgs) (*sdkmcp.CallToolResult, discordpkg.BulkDeleteResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, discordpkg.BulkDeleteResult{}, err
		}
		result, err := manager.BulkDeleteMessages(ctx, discordpkg.BulkDeleteRequest{
			ChannelID: args.ChannelID,
			UserID:    args.UserID,
			BeforeID:  args.BeforeID,
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_message_react",
		Description: "Add an emoji reaction to a Discord message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args reactArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.ReactToManagedMessage(ctx, discordpkg.ReactRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			Emoji:     args.Emoji,
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_embed_post",
		Description: "Post an embed message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args embedPostArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := manager.PostEmbed(ctx, discordpkg.EmbedPostRequest{
			ChannelID:   args.ChannelID,
			Title:       args.Title,
			Description: args.Description,
			Color:       args.Color,
			Image:       args.Image,
			Fields:      args.Fields,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}
		return nil, sendMessageResult{OK: true, ChannelID: sent.ChannelID, MessageID: sent.MessageID}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_embed_add_button",
		Description: "Append a button row to a message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args buttonAddArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := manager.AddButton(ctx, discordpkg.ButtonAddRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			Label:     args.Label,
			CustomID:  args.CustomID,
			Style:     args.Style,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}
		return nil, sendMessageResult{OK: true, ChannelID: sent.ChannelID, MessageID: sent.MessageID}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_embed_add_select",
		Description: "Append a select menu row to a message.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args selectAddArgs) (*sdkmcp.CallToolResult, sendMessageResult, error) {
		sent, err := manager.AddSelect(ctx, discordpkg.SelectAddRequest{
			ChannelID: args.ChannelID,
			MessageID: args.MessageID,
			CustomID:  args.CustomID,
			Options:   args.Options,
		})
		if err != nil {
			return nil, sendMessageResult{}, err
		}
		return nil, sendMessageResult{OK: true, ChannelID: sent.ChannelID, MessageID: sent.MessageID}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_interactions_listen",
		Description: "Listen for Discord interactions for a bounded time window.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args interactionListenArgs) (*sdkmcp.CallToolResult, interactionListenResult, error) {
		timeout := args.TimeoutSeconds
		if timeout <= 0 {
			timeout = 5
		}
		limit := args.Limit
		if limit <= 0 {
			limit = 10
		}

		listenCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()

		if err := manager.Open(listenCtx); err != nil {
			return nil, interactionListenResult{}, err
		}

		events := make(chan discordpkg.InteractionEvent, limit)
		unsubscribe := manager.SubscribeInteractions(func(_ context.Context, event discordpkg.InteractionEvent) {
			select {
			case events <- event:
			default:
			}
		})
		defer unsubscribe()

		collected := make([]discordpkg.InteractionEvent, 0, limit)
		for {
			if len(collected) >= limit {
				break
			}
			select {
			case <-listenCtx.Done():
				return nil, interactionListenResult{Events: collected}, nil
			case event := <-events:
				collected = append(collected, event)
			}
		}

		return nil, interactionListenResult{Events: collected}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_interactions_respond",
		Description: "Send a raw interaction callback response.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args interactionRespondArgs) (*sdkmcp.CallToolResult, reactResult, error) {
		err := manager.RespondInteraction(ctx, discordpkg.InteractionResponseRequest{
			InteractionID:    args.InteractionID,
			InteractionToken: args.InteractionToken,
			Type:             args.Type,
			Body:             []byte(args.Body),
		})
		return nil, reactResult{OK: err == nil}, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_rest",
		Description: "Execute a raw Discord REST request.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args restArgs) (*sdkmcp.CallToolResult, discordpkg.RESTResponse, error) {
		result, err := manager.REST(ctx, discordpkg.RESTRequest{
			Method: args.Method,
			Path:   args.Path,
			Body:   []byte(args.Body),
		})
		return nil, result, err
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_scaffold",
		Description: "Diff or apply declarative Discord guild state from YAML.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args scaffoldArgs) (*sdkmcp.CallToolResult, scaffoldResult, error) {
		spec, err := scaffoldpkg.LoadSpec(args.Path)
		if err != nil {
			return nil, scaffoldResult{}, err
		}

		if !args.Apply {
			plan, err := scaffoldpkg.BuildPlan(ctx, manager, spec)
			if err != nil {
				return nil, scaffoldResult{}, err
			}
			plan = scaffoldpkg.PreparePlan(plan, scaffoldpkg.ApplyOptions{
				DeleteExtras: args.DeleteExtras,
			})
			return nil, scaffoldResult{Applied: false, Plan: plan}, nil
		}

		if err := requireConfirm(args.Confirm); err != nil {
			return nil, scaffoldResult{}, err
		}

		plan, err := scaffoldpkg.Apply(ctx, manager, spec, scaffoldpkg.ApplyOptions{
			DeleteExtras: args.DeleteExtras,
		})
		if err != nil {
			return nil, scaffoldResult{}, err
		}
		return nil, scaffoldResult{Applied: true, Plan: plan}, nil
	})

	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "manage_exec",
		Description: "Execute Go code through yaegi with a ManagerClient surface.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args execArgs) (*sdkmcp.CallToolResult, execResult, error) {
		if err := requireConfirm(args.Confirm); err != nil {
			return nil, execResult{}, err
		}
		source := args.Source
		if args.Path != "" {
			payload, err := os.ReadFile(args.Path)
			if err != nil {
				return nil, execResult{}, err
			}
			source = string(payload)
		}
		if err := runtimeexec.ExecuteSource(ctx, manager, source); err != nil {
			return nil, execResult{}, err
		}
		return nil, execResult{OK: true}, nil
	})
}

func stringPointer(value string) *string {
	return &value
}

func requireConfirm(confirm bool) error {
	if confirm {
		return nil
	}
	return fmt.Errorf(destructiveConfirmError)
}

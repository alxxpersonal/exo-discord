package discord

import (
	"context"
	"encoding/json"
)

// --- Manager Interface ---

// Manager defines the Discord management operations used by the CLI and MCP.
type Manager interface {
	Open(context.Context) error
	Close(context.Context) error
	Mode() string
	SubscribeInteractions(InteractionHandler) func()
	GetGuild(context.Context, string) (GuildInfo, error)
	ListChannels(context.Context, string) ([]GuildChannel, error)
	ListRoles(context.Context, string) ([]GuildRole, error)
	ListMembers(context.Context, ListMembersRequest) ([]GuildMember, error)
	GetMember(context.Context, GetMemberRequest) (GuildMember, error)
	CreateChannel(context.Context, ChannelCreateRequest) (GuildChannel, error)
	UpdateChannel(context.Context, ChannelUpdateRequest) (GuildChannel, error)
	DeleteChannel(context.Context, string) error
	SetChannelPermission(context.Context, ChannelPermissionSetRequest) error
	RemoveChannelPermission(context.Context, ChannelPermissionRemoveRequest) error
	CreateRole(context.Context, RoleCreateRequest) (GuildRole, error)
	UpdateRole(context.Context, RoleUpdateRequest) (GuildRole, error)
	DeleteRole(context.Context, RoleDeleteRequest) error
	AssignRole(context.Context, RoleAssignmentRequest) error
	UnassignRole(context.Context, RoleAssignmentRequest) error
	KickMember(context.Context, GuildUserRequest) error
	BanMember(context.Context, GuildUserRequest) error
	SendManagedMessage(context.Context, SendRequest) (SentMessage, error)
	EditManagedMessage(context.Context, EditRequest) (SentMessage, error)
	DeleteManagedMessage(context.Context, MessageTarget) error
	BulkDeleteMessages(context.Context, BulkDeleteRequest) (BulkDeleteResult, error)
	ReactToManagedMessage(context.Context, ReactRequest) error
	PostEmbed(context.Context, EmbedPostRequest) (SentMessage, error)
	AddButton(context.Context, ButtonAddRequest) (SentMessage, error)
	AddSelect(context.Context, SelectAddRequest) (SentMessage, error)
	RespondInteraction(context.Context, InteractionResponseRequest) error
	REST(context.Context, RESTRequest) (RESTResponse, error)
}

// --- Manager Model Types ---

// GuildInfo stores normalized guild metadata.
type GuildInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	OwnerID      string `json:"owner_id,omitempty"`
	MemberCount  int    `json:"member_count,omitempty"`
	ChannelCount int    `json:"channel_count,omitempty"`
	RoleCount    int    `json:"role_count,omitempty"`
}

// GuildChannel stores normalized channel metadata.
type GuildChannel struct {
	ID                   string                `json:"id"`
	GuildID              string                `json:"guild_id,omitempty"`
	Name                 string                `json:"name"`
	Type                 string                `json:"type"`
	Topic                string                `json:"topic,omitempty"`
	ParentID             string                `json:"parent_id,omitempty"`
	Position             int                   `json:"position,omitempty"`
	NSFW                 bool                  `json:"nsfw,omitempty"`
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites,omitempty"`
}

// PermissionOverwrite stores normalized channel permission overwrite metadata.
type PermissionOverwrite struct {
	TargetID   string   `json:"target_id"`
	TargetType string   `json:"target_type"`
	Allow      int64    `json:"allow"`
	Deny       int64    `json:"deny"`
	AllowNames []string `json:"allow_names,omitempty"`
	DenyNames  []string `json:"deny_names,omitempty"`
}

// GuildRole stores normalized role metadata.
type GuildRole struct {
	ID              string   `json:"id"`
	GuildID         string   `json:"guild_id,omitempty"`
	Name            string   `json:"name"`
	Color           int      `json:"color,omitempty"`
	ColorHex        string   `json:"color_hex,omitempty"`
	Hoist           bool     `json:"hoist,omitempty"`
	Mentionable     bool     `json:"mentionable,omitempty"`
	Position        int      `json:"position,omitempty"`
	Permissions     int64    `json:"permissions,omitempty"`
	PermissionNames []string `json:"permission_names,omitempty"`
}

// GuildMember stores normalized member metadata.
type GuildMember struct {
	GuildID     string   `json:"guild_id,omitempty"`
	UserID      string   `json:"user_id"`
	Username    string   `json:"username,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Nick        string   `json:"nick,omitempty"`
	Bot         bool     `json:"bot,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	JoinedAt    string   `json:"joined_at,omitempty"`
}

// ListMembersRequest stores a list-members request.
type ListMembersRequest struct {
	GuildID string `json:"guild_id"`
	Limit   int    `json:"limit,omitempty"`
}

// GetMemberRequest stores a get-member request.
type GetMemberRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

// GuildUserRequest stores a guild-user action request.
type GuildUserRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
}

// ChannelCreateRequest stores a create-channel request.
type ChannelCreateRequest struct {
	GuildID  string `json:"guild_id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
	Topic    string `json:"topic,omitempty"`
}

// ChannelUpdateRequest stores an update-channel request.
type ChannelUpdateRequest struct {
	ID       string  `json:"id"`
	Name     *string `json:"name,omitempty"`
	Topic    *string `json:"topic,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

// ChannelPermissionSetRequest stores a permission overwrite upsert request.
type ChannelPermissionSetRequest struct {
	ChannelID  string   `json:"channel_id"`
	TargetID   string   `json:"target_id"`
	TargetType string   `json:"target_type"`
	Allow      []string `json:"allow,omitempty"`
	Deny       []string `json:"deny,omitempty"`
}

// ChannelPermissionRemoveRequest stores a permission overwrite delete request.
type ChannelPermissionRemoveRequest struct {
	ChannelID  string `json:"channel_id"`
	TargetID   string `json:"target_id"`
	TargetType string `json:"target_type"`
}

// RoleCreateRequest stores a create-role request.
type RoleCreateRequest struct {
	GuildID     string `json:"guild_id"`
	Name        string `json:"name"`
	Color       *int   `json:"color,omitempty"`
	Hoist       *bool  `json:"hoist,omitempty"`
	Mentionable *bool  `json:"mentionable,omitempty"`
}

// RoleUpdateRequest stores an update-role request.
type RoleUpdateRequest struct {
	GuildID     string  `json:"guild_id"`
	RoleID      string  `json:"role_id"`
	Name        *string `json:"name,omitempty"`
	Color       *int    `json:"color,omitempty"`
	Hoist       *bool   `json:"hoist,omitempty"`
	Mentionable *bool   `json:"mentionable,omitempty"`
}

// RoleDeleteRequest stores a delete-role request.
type RoleDeleteRequest struct {
	GuildID string `json:"guild_id"`
	RoleID  string `json:"role_id"`
}

// RoleAssignmentRequest stores a role assignment request.
type RoleAssignmentRequest struct {
	GuildID string `json:"guild_id"`
	UserID  string `json:"user_id"`
	RoleID  string `json:"role_id"`
}

// MessageTarget stores a channel-message identifier.
type MessageTarget struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

// BulkDeleteRequest stores a bulk delete request.
type BulkDeleteRequest struct {
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	BeforeID  string `json:"before_id,omitempty"`
}

// BulkDeleteResult stores a bulk delete result.
type BulkDeleteResult struct {
	Deleted    int      `json:"deleted"`
	MessageIDs []string `json:"message_ids,omitempty"`
}

// EmbedField stores a single embed field.
type EmbedField struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// EmbedPostRequest stores an embed send request.
type EmbedPostRequest struct {
	ChannelID   string       `json:"channel_id"`
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       *int         `json:"color,omitempty"`
	Image       string       `json:"image,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
}

// ButtonAddRequest stores a button append request.
type ButtonAddRequest struct {
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
	Label     string `json:"label"`
	CustomID  string `json:"custom_id"`
	Style     string `json:"style"`
}

// SelectOption stores a select option.
type SelectOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SelectAddRequest stores a select append request.
type SelectAddRequest struct {
	ChannelID string         `json:"channel_id"`
	MessageID string         `json:"message_id"`
	CustomID  string         `json:"custom_id"`
	Options   []SelectOption `json:"options"`
}

// InteractionEvent stores normalized interaction metadata.
type InteractionEvent struct {
	ID            string          `json:"id"`
	Type          int             `json:"type"`
	ApplicationID string          `json:"application_id,omitempty"`
	GuildID       string          `json:"guild_id,omitempty"`
	ChannelID     string          `json:"channel_id,omitempty"`
	Token         string          `json:"token,omitempty"`
	Name          string          `json:"name,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
}

// InteractionResponseRequest stores a raw interaction response request.
type InteractionResponseRequest struct {
	InteractionID    string          `json:"interaction_id"`
	InteractionToken string          `json:"interaction_token"`
	Type             int             `json:"type"`
	Body             json.RawMessage `json:"body,omitempty"`
}

// RESTRequest stores a raw REST request.
type RESTRequest struct {
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// RESTResponse stores a raw REST response.
type RESTResponse struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	Body   string `json:"body,omitempty"`
}

// InteractionHandler handles a normalized interaction event.
type InteractionHandler func(context.Context, InteractionEvent)

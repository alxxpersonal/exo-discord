package mcp

import (
	"context"
	"errors"
	"os"
	"slices"
	"testing"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Test Doubles ---

type fakeSession struct {
	sendRequest     discordpkg.SendRequest
	replyRequest    discordpkg.ReplyRequest
	reactRequest    discordpkg.ReactRequest
	editRequest     discordpkg.EditRequest
	historyRequest  discordpkg.HistoryRequest
	downloadRequest discordpkg.DownloadRequest
	statusRequest   discordpkg.StatusRequest
	guildID         string
	listMembersReq  discordpkg.ListMembersRequest
	getMemberReq    discordpkg.GetMemberRequest
	channelCreate   discordpkg.ChannelCreateRequest
	channelUpdate   discordpkg.ChannelUpdateRequest
	channelDeleteID string
	permSet         discordpkg.ChannelPermissionSetRequest
	permRemove      discordpkg.ChannelPermissionRemoveRequest
	roleCreate      discordpkg.RoleCreateRequest
	roleUpdate      discordpkg.RoleUpdateRequest
	roleDelete      discordpkg.RoleDeleteRequest
	roleAssign      discordpkg.RoleAssignmentRequest
	memberRequest   discordpkg.GuildUserRequest
	messageDelete   discordpkg.MessageTarget
	bulkDelete      discordpkg.BulkDeleteRequest
	embedPost       discordpkg.EmbedPostRequest
	buttonAdd       discordpkg.ButtonAddRequest
	selectAdd       discordpkg.SelectAddRequest
	interactionResp discordpkg.InteractionResponseRequest
	restRequest     discordpkg.RESTRequest
	interactionHook discordpkg.InteractionHandler
	onSubscribe     func(discordpkg.InteractionHandler)
}

type errorSession struct {
	err error
}

func (e *errorSession) Open(context.Context) error                 { return nil }
func (e *errorSession) Close(context.Context) error                { return nil }
func (e *errorSession) Mode() string                               { return "bot" }
func (e *errorSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }
func (e *errorSession) SendMessage(context.Context, discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) Reply(context.Context, discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) React(context.Context, discordpkg.ReactRequest) error { return e.err }
func (e *errorSession) EditMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) FetchHistory(context.Context, discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	return nil, e.err
}
func (e *errorSession) DownloadAttachments(context.Context, discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	return nil, e.err
}
func (e *errorSession) SetStatus(context.Context, discordpkg.StatusRequest) error { return e.err }
func (e *errorSession) SubscribeInteractions(discordpkg.InteractionHandler) func() {
	return func() {}
}
func (e *errorSession) GetGuild(context.Context, string) (discordpkg.GuildInfo, error) {
	return discordpkg.GuildInfo{}, e.err
}
func (e *errorSession) ListChannels(context.Context, string) ([]discordpkg.GuildChannel, error) {
	return nil, e.err
}
func (e *errorSession) ListRoles(context.Context, string) ([]discordpkg.GuildRole, error) {
	return nil, e.err
}
func (e *errorSession) ListMembers(context.Context, discordpkg.ListMembersRequest) ([]discordpkg.GuildMember, error) {
	return nil, e.err
}
func (e *errorSession) GetMember(context.Context, discordpkg.GetMemberRequest) (discordpkg.GuildMember, error) {
	return discordpkg.GuildMember{}, e.err
}
func (e *errorSession) CreateChannel(context.Context, discordpkg.ChannelCreateRequest) (discordpkg.GuildChannel, error) {
	return discordpkg.GuildChannel{}, e.err
}
func (e *errorSession) UpdateChannel(context.Context, discordpkg.ChannelUpdateRequest) (discordpkg.GuildChannel, error) {
	return discordpkg.GuildChannel{}, e.err
}
func (e *errorSession) DeleteChannel(context.Context, string) error { return e.err }
func (e *errorSession) SetChannelPermission(context.Context, discordpkg.ChannelPermissionSetRequest) error {
	return e.err
}
func (e *errorSession) RemoveChannelPermission(context.Context, discordpkg.ChannelPermissionRemoveRequest) error {
	return e.err
}
func (e *errorSession) CreateRole(context.Context, discordpkg.RoleCreateRequest) (discordpkg.GuildRole, error) {
	return discordpkg.GuildRole{}, e.err
}
func (e *errorSession) UpdateRole(context.Context, discordpkg.RoleUpdateRequest) (discordpkg.GuildRole, error) {
	return discordpkg.GuildRole{}, e.err
}
func (e *errorSession) DeleteRole(context.Context, discordpkg.RoleDeleteRequest) error { return e.err }
func (e *errorSession) AssignRole(context.Context, discordpkg.RoleAssignmentRequest) error {
	return e.err
}
func (e *errorSession) UnassignRole(context.Context, discordpkg.RoleAssignmentRequest) error {
	return e.err
}
func (e *errorSession) KickMember(context.Context, discordpkg.GuildUserRequest) error { return e.err }
func (e *errorSession) BanMember(context.Context, discordpkg.GuildUserRequest) error  { return e.err }
func (e *errorSession) SendManagedMessage(context.Context, discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) EditManagedMessage(context.Context, discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) DeleteManagedMessage(context.Context, discordpkg.MessageTarget) error {
	return e.err
}
func (e *errorSession) BulkDeleteMessages(context.Context, discordpkg.BulkDeleteRequest) (discordpkg.BulkDeleteResult, error) {
	return discordpkg.BulkDeleteResult{}, e.err
}
func (e *errorSession) ReactToManagedMessage(context.Context, discordpkg.ReactRequest) error {
	return e.err
}
func (e *errorSession) PostEmbed(context.Context, discordpkg.EmbedPostRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) AddButton(context.Context, discordpkg.ButtonAddRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) AddSelect(context.Context, discordpkg.SelectAddRequest) (discordpkg.SentMessage, error) {
	return discordpkg.SentMessage{}, e.err
}
func (e *errorSession) RespondInteraction(context.Context, discordpkg.InteractionResponseRequest) error {
	return e.err
}
func (e *errorSession) REST(context.Context, discordpkg.RESTRequest) (discordpkg.RESTResponse, error) {
	return discordpkg.RESTResponse{}, e.err
}

func (f *fakeSession) Open(context.Context) error                 { return nil }
func (f *fakeSession) Close(context.Context) error                { return nil }
func (f *fakeSession) Mode() string                               { return "bot" }
func (f *fakeSession) Subscribe(discordpkg.InboundHandler) func() { return func() {} }

func (f *fakeSession) SendMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	f.sendRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "send-1"}, nil
}

func (f *fakeSession) Reply(_ context.Context, req discordpkg.ReplyRequest) (discordpkg.SentMessage, error) {
	f.replyRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "reply-1"}, nil
}

func (f *fakeSession) React(_ context.Context, req discordpkg.ReactRequest) error {
	f.reactRequest = req
	return nil
}

func (f *fakeSession) EditMessage(_ context.Context, req discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	f.editRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) FetchHistory(_ context.Context, req discordpkg.HistoryRequest) ([]discordpkg.Message, error) {
	f.historyRequest = req
	return []discordpkg.Message{{ID: "hist-1"}}, nil
}

func (f *fakeSession) DownloadAttachments(_ context.Context, req discordpkg.DownloadRequest) ([]discordpkg.DownloadedFile, error) {
	f.downloadRequest = req
	return []discordpkg.DownloadedFile{{AttachmentID: "att-1", Path: req.DestinationDir + "/att-1.png"}}, nil
}

func (f *fakeSession) SetStatus(_ context.Context, req discordpkg.StatusRequest) error {
	f.statusRequest = req
	return nil
}

func (f *fakeSession) SubscribeInteractions(handler discordpkg.InteractionHandler) func() {
	f.interactionHook = handler
	if f.onSubscribe != nil {
		f.onSubscribe(handler)
	}
	return func() {
		f.interactionHook = nil
	}
}

func (f *fakeSession) GetGuild(_ context.Context, guildID string) (discordpkg.GuildInfo, error) {
	f.guildID = guildID
	return discordpkg.GuildInfo{ID: guildID, Name: "Guild"}, nil
}

func (f *fakeSession) ListChannels(_ context.Context, guildID string) ([]discordpkg.GuildChannel, error) {
	f.guildID = guildID
	return []discordpkg.GuildChannel{{ID: "chan-1", GuildID: guildID, Name: "general", Type: "text"}}, nil
}

func (f *fakeSession) ListRoles(_ context.Context, guildID string) ([]discordpkg.GuildRole, error) {
	f.guildID = guildID
	return []discordpkg.GuildRole{{ID: "role-1", GuildID: guildID, Name: "admin"}}, nil
}

func (f *fakeSession) ListMembers(_ context.Context, req discordpkg.ListMembersRequest) ([]discordpkg.GuildMember, error) {
	f.listMembersReq = req
	return []discordpkg.GuildMember{{GuildID: req.GuildID, UserID: "user-1"}}, nil
}

func (f *fakeSession) GetMember(_ context.Context, req discordpkg.GetMemberRequest) (discordpkg.GuildMember, error) {
	f.getMemberReq = req
	return discordpkg.GuildMember{GuildID: req.GuildID, UserID: req.UserID}, nil
}

func (f *fakeSession) CreateChannel(_ context.Context, req discordpkg.ChannelCreateRequest) (discordpkg.GuildChannel, error) {
	f.channelCreate = req
	return discordpkg.GuildChannel{ID: "chan-created", GuildID: req.GuildID, Name: req.Name, Type: req.Type}, nil
}

func (f *fakeSession) UpdateChannel(_ context.Context, req discordpkg.ChannelUpdateRequest) (discordpkg.GuildChannel, error) {
	f.channelUpdate = req
	return discordpkg.GuildChannel{ID: req.ID, Name: "updated", Type: "text"}, nil
}

func (f *fakeSession) DeleteChannel(_ context.Context, channelID string) error {
	f.channelDeleteID = channelID
	return nil
}

func (f *fakeSession) SetChannelPermission(_ context.Context, req discordpkg.ChannelPermissionSetRequest) error {
	f.permSet = req
	return nil
}

func (f *fakeSession) RemoveChannelPermission(_ context.Context, req discordpkg.ChannelPermissionRemoveRequest) error {
	f.permRemove = req
	return nil
}

func (f *fakeSession) CreateRole(_ context.Context, req discordpkg.RoleCreateRequest) (discordpkg.GuildRole, error) {
	f.roleCreate = req
	return discordpkg.GuildRole{ID: "role-created", GuildID: req.GuildID, Name: req.Name}, nil
}

func (f *fakeSession) UpdateRole(_ context.Context, req discordpkg.RoleUpdateRequest) (discordpkg.GuildRole, error) {
	f.roleUpdate = req
	return discordpkg.GuildRole{ID: req.RoleID, GuildID: req.GuildID, Name: req.RoleID}, nil
}

func (f *fakeSession) DeleteRole(_ context.Context, req discordpkg.RoleDeleteRequest) error {
	f.roleDelete = req
	return nil
}

func (f *fakeSession) AssignRole(_ context.Context, req discordpkg.RoleAssignmentRequest) error {
	f.roleAssign = req
	return nil
}

func (f *fakeSession) UnassignRole(_ context.Context, req discordpkg.RoleAssignmentRequest) error {
	f.roleAssign = req
	return nil
}

func (f *fakeSession) KickMember(_ context.Context, req discordpkg.GuildUserRequest) error {
	f.memberRequest = req
	return nil
}

func (f *fakeSession) BanMember(_ context.Context, req discordpkg.GuildUserRequest) error {
	f.memberRequest = req
	return nil
}

func (f *fakeSession) SendManagedMessage(_ context.Context, req discordpkg.SendRequest) (discordpkg.SentMessage, error) {
	f.sendRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "managed-send"}, nil
}

func (f *fakeSession) EditManagedMessage(_ context.Context, req discordpkg.EditRequest) (discordpkg.SentMessage, error) {
	f.editRequest = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) DeleteManagedMessage(_ context.Context, req discordpkg.MessageTarget) error {
	f.messageDelete = req
	return nil
}

func (f *fakeSession) BulkDeleteMessages(_ context.Context, req discordpkg.BulkDeleteRequest) (discordpkg.BulkDeleteResult, error) {
	f.bulkDelete = req
	return discordpkg.BulkDeleteResult{Deleted: 2, MessageIDs: []string{"m1", "m2"}}, nil
}

func (f *fakeSession) ReactToManagedMessage(_ context.Context, req discordpkg.ReactRequest) error {
	f.reactRequest = req
	return nil
}

func (f *fakeSession) PostEmbed(_ context.Context, req discordpkg.EmbedPostRequest) (discordpkg.SentMessage, error) {
	f.embedPost = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: "embed-1"}, nil
}

func (f *fakeSession) AddButton(_ context.Context, req discordpkg.ButtonAddRequest) (discordpkg.SentMessage, error) {
	f.buttonAdd = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) AddSelect(_ context.Context, req discordpkg.SelectAddRequest) (discordpkg.SentMessage, error) {
	f.selectAdd = req
	return discordpkg.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeSession) RespondInteraction(_ context.Context, req discordpkg.InteractionResponseRequest) error {
	f.interactionResp = req
	return nil
}

func (f *fakeSession) REST(_ context.Context, req discordpkg.RESTRequest) (discordpkg.RESTResponse, error) {
	f.restRequest = req
	return discordpkg.RESTResponse{Method: req.Method, Path: req.Path, Status: 200, Body: "{}"}, nil
}

// --- Test Cases ---

func TestServerListTools(t *testing.T) {
	t.Parallel()

	server, clientSession := connectTestServer(t, &fakeSession{})
	t.Cleanup(func() {
		_ = clientSession.Close()
	})
	_ = server

	result, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	if got, want := len(result.Tools), 40; got != want {
		t.Fatalf("tool count = %d, want %d", got, want)
	}

	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	for _, required := range []string{
		"send_message",
		"manage_channel_create",
		"manage_role_create",
		"manage_message_bulk_delete",
		"manage_embed_post",
		"manage_rest",
		"manage_scaffold",
		"manage_exec",
	} {
		if !slices.Contains(names, required) {
			t.Fatalf("tool names = %v, missing %q", names, required)
		}
	}
}

func TestServerRaw(t *testing.T) {
	t.Parallel()

	runtime := &fakeSession{}
	server := NewServer(runtime, runtime)
	if server.Raw() == nil {
		t.Fatal("Raw() = nil, want server")
	}
}

func TestServerCallTools(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tool  string
		args  map[string]any
		check func(t *testing.T, session *fakeSession)
	}{
		{
			name: "send",
			tool: "send_message",
			args: map[string]any{"channel_id": "chan-1", "text": "hello"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.sendRequest.ChannelID != "chan-1" || session.sendRequest.Text != "hello" {
					t.Fatalf("send request = %#v", session.sendRequest)
				}
			},
		},
		{
			name: "reply",
			tool: "reply",
			args: map[string]any{"channel_id": "chan-1", "reply_to_message_id": "msg-1", "text": "reply"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.replyRequest.ReplyToMessageID != "msg-1" {
					t.Fatalf("reply request = %#v", session.replyRequest)
				}
			},
		},
		{
			name: "react",
			tool: "react",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "emoji": "👍"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.reactRequest.Emoji != "👍" {
					t.Fatalf("react request = %#v", session.reactRequest)
				}
			},
		},
		{
			name: "fetch-history",
			tool: "fetch_history",
			args: map[string]any{"channel_id": "chan-1", "limit": 5},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.historyRequest.Limit != 5 {
					t.Fatalf("history request = %#v", session.historyRequest)
				}
			},
		},
		{
			name: "download",
			tool: "download_attachment",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "destination_dir": "/tmp/inbox", "max_bytes": 2048},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.downloadRequest.DestinationDir != "/tmp/inbox" || session.downloadRequest.MaxBytes != 2048 {
					t.Fatalf("download request = %#v", session.downloadRequest)
				}
			},
		},
		{
			name: "edit",
			tool: "edit_message",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "text": "edited"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.editRequest.Text != "edited" {
					t.Fatalf("edit request = %#v", session.editRequest)
				}
			},
		},
		{
			name: "status",
			tool: "set_status",
			args: map[string]any{"presence": "online", "activity_type": "watching", "activity_text": "tests"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.statusRequest.ActivityText != "tests" {
					t.Fatalf("status request = %#v", session.statusRequest)
				}
			},
		},
		{
			name: "manage-guild-list-channels",
			tool: "manage_guild_list_channels",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.guildID != "guild-1" {
					t.Fatalf("guildID = %q", session.guildID)
				}
			},
		},
		{
			name: "manage-guild-list-roles",
			tool: "manage_guild_list_roles",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.guildID != "guild-1" {
					t.Fatalf("guildID = %q", session.guildID)
				}
			},
		},
		{
			name: "manage-guild-list-members",
			tool: "manage_guild_list_members",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.listMembersReq.GuildID != "guild-1" {
					t.Fatalf("listMembersReq = %#v", session.listMembersReq)
				}
			},
		},
		{
			name: "manage-guild-inspect",
			tool: "manage_guild_inspect",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.guildID != "guild-1" {
					t.Fatalf("guildID = %q", session.guildID)
				}
			},
		},
		{
			name: "manage-channel-list",
			tool: "manage_channel_list",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.guildID != "guild-1" {
					t.Fatalf("guildID = %q", session.guildID)
				}
			},
		},
		{
			name: "manage-channel-create",
			tool: "manage_channel_create",
			args: map[string]any{"guild_id": "guild-1", "name": "ops", "type": "text"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.channelCreate.GuildID != "guild-1" || session.channelCreate.Name != "ops" {
					t.Fatalf("channel create = %#v", session.channelCreate)
				}
			},
		},
		{
			name: "manage-channel-update",
			tool: "manage_channel_update",
			args: map[string]any{"channel_id": "chan-1", "name": "ops-2"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.channelUpdate.ID != "chan-1" || session.channelUpdate.Name == nil || *session.channelUpdate.Name != "ops-2" {
					t.Fatalf("channel update = %#v", session.channelUpdate)
				}
			},
		},
		{
			name: "manage-channel-delete",
			tool: "manage_channel_delete",
			args: map[string]any{"channel_id": "chan-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.channelDeleteID != "chan-1" {
					t.Fatalf("channelDeleteID = %q", session.channelDeleteID)
				}
			},
		},
		{
			name: "manage-channel-perm-set",
			tool: "manage_channel_perm_set",
			args: map[string]any{"channel_id": "chan-1", "target_id": "role-1", "target_type": "role", "allow": []string{"view_channel"}},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.permSet.ChannelID != "chan-1" || session.permSet.TargetID != "role-1" {
					t.Fatalf("permSet = %#v", session.permSet)
				}
			},
		},
		{
			name: "manage-channel-perm-remove",
			tool: "manage_channel_perm_remove",
			args: map[string]any{"channel_id": "chan-1", "target_id": "role-1", "target_type": "role"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.permRemove.ChannelID != "chan-1" || session.permRemove.TargetID != "role-1" {
					t.Fatalf("permRemove = %#v", session.permRemove)
				}
			},
		},
		{
			name: "manage-role-list",
			tool: "manage_role_list",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.guildID != "guild-1" {
					t.Fatalf("guildID = %q", session.guildID)
				}
			},
		},
		{
			name: "manage-role-create",
			tool: "manage_role_create",
			args: map[string]any{"guild_id": "guild-1", "name": "admin"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.roleCreate.Name != "admin" || session.roleCreate.GuildID != "guild-1" {
					t.Fatalf("role create = %#v", session.roleCreate)
				}
			},
		},
		{
			name: "manage-role-update",
			tool: "manage_role_update",
			args: map[string]any{"role_id": "role-1", "name": "ops-2"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.roleUpdate.RoleID != "role-1" || session.roleUpdate.Name == nil || *session.roleUpdate.Name != "ops-2" {
					t.Fatalf("roleUpdate = %#v", session.roleUpdate)
				}
			},
		},
		{
			name: "manage-role-delete",
			tool: "manage_role_delete",
			args: map[string]any{"role_id": "role-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.roleDelete.RoleID != "role-1" {
					t.Fatalf("roleDelete = %#v", session.roleDelete)
				}
			},
		},
		{
			name: "manage-role-assign",
			tool: "manage_role_assign",
			args: map[string]any{"user_id": "user-1", "role_id": "role-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.roleAssign.UserID != "user-1" || session.roleAssign.RoleID != "role-1" {
					t.Fatalf("roleAssign = %#v", session.roleAssign)
				}
			},
		},
		{
			name: "manage-role-unassign",
			tool: "manage_role_unassign",
			args: map[string]any{"user_id": "user-1", "role_id": "role-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.roleAssign.UserID != "user-1" || session.roleAssign.RoleID != "role-1" {
					t.Fatalf("roleAssign = %#v", session.roleAssign)
				}
			},
		},
		{
			name: "manage-member-list",
			tool: "manage_member_list",
			args: map[string]any{"guild_id": "guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.listMembersReq.GuildID != "guild-1" {
					t.Fatalf("listMembersReq = %#v", session.listMembersReq)
				}
			},
		},
		{
			name: "manage-member-get",
			tool: "manage_member_get",
			args: map[string]any{"guild_id": "guild-1", "user_id": "user-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.getMemberReq.GuildID != "guild-1" || session.getMemberReq.UserID != "user-1" {
					t.Fatalf("getMemberReq = %#v", session.getMemberReq)
				}
			},
		},
		{
			name: "manage-member-kick",
			tool: "manage_member_kick",
			args: map[string]any{"guild_id": "guild-1", "user_id": "user-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.memberRequest.GuildID != "guild-1" || session.memberRequest.UserID != "user-1" {
					t.Fatalf("memberRequest = %#v", session.memberRequest)
				}
			},
		},
		{
			name: "manage-member-ban",
			tool: "manage_member_ban",
			args: map[string]any{"guild_id": "guild-1", "user_id": "user-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.memberRequest.GuildID != "guild-1" || session.memberRequest.UserID != "user-1" {
					t.Fatalf("memberRequest = %#v", session.memberRequest)
				}
			},
		},
		{
			name: "manage-message-send",
			tool: "manage_message_send",
			args: map[string]any{"channel_id": "chan-1", "text": "hello"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.sendRequest.ChannelID != "chan-1" || session.sendRequest.Text != "hello" {
					t.Fatalf("sendRequest = %#v", session.sendRequest)
				}
			},
		},
		{
			name: "manage-message-edit",
			tool: "manage_message_edit",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "text": "updated"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.editRequest.MessageID != "msg-1" || session.editRequest.Text != "updated" {
					t.Fatalf("editRequest = %#v", session.editRequest)
				}
			},
		},
		{
			name: "manage-message-delete",
			tool: "manage_message_delete",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.messageDelete.ChannelID != "chan-1" || session.messageDelete.MessageID != "msg-1" {
					t.Fatalf("messageDelete = %#v", session.messageDelete)
				}
			},
		},
		{
			name: "manage-message-bulk-delete",
			tool: "manage_message_bulk_delete",
			args: map[string]any{"channel_id": "chan-1", "user_id": "user-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.bulkDelete.ChannelID != "chan-1" || session.bulkDelete.UserID != "user-1" {
					t.Fatalf("bulk delete = %#v", session.bulkDelete)
				}
			},
		},
		{
			name: "manage-message-react",
			tool: "manage_message_react",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "emoji": "👍"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.reactRequest.Emoji != "👍" || session.reactRequest.MessageID != "msg-1" {
					t.Fatalf("reactRequest = %#v", session.reactRequest)
				}
			},
		},
		{
			name: "manage-embed-post",
			tool: "manage_embed_post",
			args: map[string]any{"channel_id": "chan-1", "title": "hello"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.embedPost.ChannelID != "chan-1" || session.embedPost.Title != "hello" {
					t.Fatalf("embedPost = %#v", session.embedPost)
				}
			},
		},
		{
			name: "manage-embed-add-button",
			tool: "manage_embed_add_button",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "label": "Go", "custom_id": "go-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.buttonAdd.CustomID != "go-1" || session.buttonAdd.MessageID != "msg-1" {
					t.Fatalf("buttonAdd = %#v", session.buttonAdd)
				}
			},
		},
		{
			name: "manage-embed-add-select",
			tool: "manage_embed_add_select",
			args: map[string]any{"channel_id": "chan-1", "message_id": "msg-1", "custom_id": "sel-1", "options": []map[string]any{{"label": "one", "value": "1"}}},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.selectAdd.CustomID != "sel-1" || len(session.selectAdd.Options) != 1 {
					t.Fatalf("selectAdd = %#v", session.selectAdd)
				}
			},
		},
		{
			name: "manage-interactions-respond",
			tool: "manage_interactions_respond",
			args: map[string]any{"interaction_id": "int-1", "interaction_token": "tok-1", "type": 4, "body": "{\"content\":\"hi\"}"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.interactionResp.InteractionID != "int-1" || session.interactionResp.Type != 4 {
					t.Fatalf("interactionResp = %#v", session.interactionResp)
				}
			},
		},
		{
			name: "manage-rest",
			tool: "manage_rest",
			args: map[string]any{"method": "GET", "path": "/guilds/guild-1"},
			check: func(t *testing.T, session *fakeSession) {
				t.Helper()
				if session.restRequest.Method != "GET" || session.restRequest.Path != "/guilds/guild-1" {
					t.Fatalf("rest request = %#v", session.restRequest)
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			session := &fakeSession{}
			server, clientSession := connectTestServer(t, session)
			t.Cleanup(func() {
				_ = clientSession.Close()
			})
			_ = server

			result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      test.tool,
				Arguments: test.args,
			})
			if err != nil {
				t.Fatalf("CallTool(%s) error = %v", test.tool, err)
			}
			if result.IsError {
				t.Fatalf("CallTool(%s) returned MCP error", test.tool)
			}
			test.check(t, session)
		})
	}
}

func TestServerCallToolErrors(t *testing.T) {
	t.Parallel()

	server, clientSession := connectTestServer(t, &errorSession{err: errors.New("boom")})
	t.Cleanup(func() {
		_ = clientSession.Close()
	})
	_ = server

	result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "send_message",
		Arguments: map[string]any{"channel_id": "chan-1", "text": "hello"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatal("CallTool() IsError = false, want true")
	}
}

func TestServerCallManageInteractionListenScaffoldAndExec(t *testing.T) {
	t.Parallel()

	t.Run("listen", func(t *testing.T) {
		runtime := &fakeSession{}
		runtime.onSubscribe = func(handler discordpkg.InteractionHandler) {
			go handler(context.Background(), discordpkg.InteractionEvent{ID: "evt-1", Type: 2})
		}

		_, clientSession := connectTestServer(t, runtime)
		t.Cleanup(func() {
			_ = clientSession.Close()
		})

		result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "manage_interactions_listen",
			Arguments: map[string]any{"limit": 1, "timeout_seconds": 1},
		})
		if err != nil {
			t.Fatalf("CallTool(listen) error = %v", err)
		}
		if result.IsError {
			t.Fatal("CallTool(listen) returned MCP error")
		}
	})

	t.Run("scaffold", func(t *testing.T) {
		runtime := &fakeSession{}
		specPath := t.TempDir() + "/scaffold.yaml"
		if err := os.WriteFile(specPath, []byte("guild:\n  id: guild-1\nroles:\n  - name: admin\n"), 0o600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		_, clientSession := connectTestServer(t, runtime)
		t.Cleanup(func() {
			_ = clientSession.Close()
		})

		result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "manage_scaffold",
			Arguments: map[string]any{"path": specPath},
		})
		if err != nil {
			t.Fatalf("CallTool(scaffold) error = %v", err)
		}
		if result.IsError {
			t.Fatal("CallTool(scaffold) returned MCP error")
		}
	})

	t.Run("exec", func(t *testing.T) {
		runtime := &fakeSession{}
		_, clientSession := connectTestServer(t, runtime)
		t.Cleanup(func() {
			_ = clientSession.Close()
		})

		result, err := clientSession.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name:      "manage_exec",
			Arguments: map[string]any{"source": "package main\nfunc Run(client *ManagerClient) error { return client.DeleteChannel(\"chan-1\") }\n"},
		})
		if err != nil {
			t.Fatalf("CallTool(exec) error = %v", err)
		}
		if result.IsError {
			t.Fatal("CallTool(exec) returned MCP error")
		}
		if runtime.channelDeleteID != "chan-1" {
			t.Fatalf("channelDeleteID = %q", runtime.channelDeleteID)
		}
	})
}

// --- Helpers ---

func connectTestServer(t *testing.T, runtime interface {
	discordpkg.Session
	discordpkg.Manager
}) (*Server, *sdkmcp.ClientSession) {
	t.Helper()

	server := NewServer(runtime, runtime)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	ctx := context.Background()
	go func() {
		_ = server.Raw().Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	return server, clientSession
}

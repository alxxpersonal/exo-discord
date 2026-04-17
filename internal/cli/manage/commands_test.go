package managecmd

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeManager struct {
	guild          discord.GuildInfo
	channels       []discord.GuildChannel
	roles          []discord.GuildRole
	members        []discord.GuildMember
	guildLookup    string
	listMembersReq discord.ListMembersRequest
	getMemberReq   discord.GetMemberRequest
	channelCreate  discord.ChannelCreateRequest
	channelUpdate  discord.ChannelUpdateRequest
	channelDelete  string
	permSet        discord.ChannelPermissionSetRequest
	permRemove     discord.ChannelPermissionRemoveRequest
	roleCreate     discord.RoleCreateRequest
	roleUpdate     discord.RoleUpdateRequest
	roleDelete     discord.RoleDeleteRequest
	roleAssign     discord.RoleAssignmentRequest
	guildUserReq   discord.GuildUserRequest
	sendReq        discord.SendRequest
	editReq        discord.EditRequest
	messageDelete  discord.MessageTarget
	bulkDeleteReq  discord.BulkDeleteRequest
	reactReq       discord.ReactRequest
	embedReq       discord.EmbedPostRequest
	buttonReq      discord.ButtonAddRequest
	selectReq      discord.SelectAddRequest
	interactionReq discord.InteractionResponseRequest
	restReq        discord.RESTRequest
	openCount      int
	closeCount     int
	handler        discord.InteractionHandler
	onSubscribe    func(discord.InteractionHandler)
}

func (f *fakeManager) Open(context.Context) error {
	f.openCount++
	return nil
}

func (f *fakeManager) Close(context.Context) error {
	f.closeCount++
	return nil
}

func (f *fakeManager) Mode() string { return "bot" }

func (f *fakeManager) SubscribeInteractions(handler discord.InteractionHandler) func() {
	f.handler = handler
	if f.onSubscribe != nil {
		f.onSubscribe(handler)
	}
	return func() {
		f.handler = nil
	}
}

func (f *fakeManager) GetGuild(_ context.Context, guildID string) (discord.GuildInfo, error) {
	f.guildLookup = guildID
	if f.guild.ID == "" {
		return discord.GuildInfo{ID: guildID, Name: "Guild"}, nil
	}
	return f.guild, nil
}

func (f *fakeManager) ListChannels(_ context.Context, guildID string) ([]discord.GuildChannel, error) {
	f.guildLookup = guildID
	if f.channels == nil {
		return []discord.GuildChannel{{ID: "chan-1", GuildID: guildID, Name: "general", Type: "text"}}, nil
	}
	return f.channels, nil
}

func (f *fakeManager) ListRoles(_ context.Context, guildID string) ([]discord.GuildRole, error) {
	f.guildLookup = guildID
	if f.roles == nil {
		return []discord.GuildRole{{ID: "role-1", GuildID: guildID, Name: "admin"}}, nil
	}
	return f.roles, nil
}

func (f *fakeManager) ListMembers(_ context.Context, req discord.ListMembersRequest) ([]discord.GuildMember, error) {
	f.listMembersReq = req
	if f.members == nil {
		return []discord.GuildMember{{GuildID: req.GuildID, UserID: "user-1"}}, nil
	}
	return f.members, nil
}

func (f *fakeManager) GetMember(_ context.Context, req discord.GetMemberRequest) (discord.GuildMember, error) {
	f.getMemberReq = req
	return discord.GuildMember{GuildID: req.GuildID, UserID: req.UserID}, nil
}

func (f *fakeManager) CreateChannel(_ context.Context, req discord.ChannelCreateRequest) (discord.GuildChannel, error) {
	f.channelCreate = req
	return discord.GuildChannel{ID: "chan-created", GuildID: req.GuildID, Name: req.Name, Type: req.Type}, nil
}

func (f *fakeManager) UpdateChannel(_ context.Context, req discord.ChannelUpdateRequest) (discord.GuildChannel, error) {
	f.channelUpdate = req
	return discord.GuildChannel{ID: req.ID, Name: "updated", Type: "text"}, nil
}

func (f *fakeManager) DeleteChannel(_ context.Context, channelID string) error {
	f.channelDelete = channelID
	return nil
}

func (f *fakeManager) SetChannelPermission(_ context.Context, req discord.ChannelPermissionSetRequest) error {
	f.permSet = req
	return nil
}

func (f *fakeManager) RemoveChannelPermission(_ context.Context, req discord.ChannelPermissionRemoveRequest) error {
	f.permRemove = req
	return nil
}

func (f *fakeManager) CreateRole(_ context.Context, req discord.RoleCreateRequest) (discord.GuildRole, error) {
	f.roleCreate = req
	return discord.GuildRole{ID: "role-created", GuildID: req.GuildID, Name: req.Name}, nil
}

func (f *fakeManager) UpdateRole(_ context.Context, req discord.RoleUpdateRequest) (discord.GuildRole, error) {
	f.roleUpdate = req
	return discord.GuildRole{ID: req.RoleID, Name: "updated"}, nil
}

func (f *fakeManager) DeleteRole(_ context.Context, req discord.RoleDeleteRequest) error {
	f.roleDelete = req
	return nil
}

func (f *fakeManager) AssignRole(_ context.Context, req discord.RoleAssignmentRequest) error {
	f.roleAssign = req
	return nil
}

func (f *fakeManager) UnassignRole(_ context.Context, req discord.RoleAssignmentRequest) error {
	f.roleAssign = req
	return nil
}

func (f *fakeManager) KickMember(_ context.Context, req discord.GuildUserRequest) error {
	f.guildUserReq = req
	return nil
}

func (f *fakeManager) BanMember(_ context.Context, req discord.GuildUserRequest) error {
	f.guildUserReq = req
	return nil
}

func (f *fakeManager) SendManagedMessage(_ context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	f.sendReq = req
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: "msg-send"}, nil
}

func (f *fakeManager) EditManagedMessage(_ context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	f.editReq = req
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeManager) DeleteManagedMessage(_ context.Context, req discord.MessageTarget) error {
	f.messageDelete = req
	return nil
}

func (f *fakeManager) BulkDeleteMessages(_ context.Context, req discord.BulkDeleteRequest) (discord.BulkDeleteResult, error) {
	f.bulkDeleteReq = req
	return discord.BulkDeleteResult{Deleted: 2, MessageIDs: []string{"m1", "m2"}}, nil
}

func (f *fakeManager) ReactToManagedMessage(_ context.Context, req discord.ReactRequest) error {
	f.reactReq = req
	return nil
}

func (f *fakeManager) PostEmbed(_ context.Context, req discord.EmbedPostRequest) (discord.SentMessage, error) {
	f.embedReq = req
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: "embed-1"}, nil
}

func (f *fakeManager) AddButton(_ context.Context, req discord.ButtonAddRequest) (discord.SentMessage, error) {
	f.buttonReq = req
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeManager) AddSelect(_ context.Context, req discord.SelectAddRequest) (discord.SentMessage, error) {
	f.selectReq = req
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: req.MessageID}, nil
}

func (f *fakeManager) RespondInteraction(_ context.Context, req discord.InteractionResponseRequest) error {
	f.interactionReq = req
	return nil
}

func (f *fakeManager) REST(_ context.Context, req discord.RESTRequest) (discord.RESTResponse, error) {
	f.restReq = req
	return discord.RESTResponse{Method: req.Method, Path: req.Path, Status: 200, Body: "{}"}, nil
}

// --- Tests ---

func TestManageCommandDispatchesSubcommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		args  []string
		check func(t *testing.T, manager *fakeManager, stdout string)
	}{
		{
			name: "guild-list-channels",
			args: []string{"guild", "guild-1", "list-channels"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildLookup != "guild-1" || !strings.Contains(stdout, "general") {
					t.Fatalf("guildLookup=%q stdout=%q", manager.guildLookup, stdout)
				}
			},
		},
		{
			name: "guild-list-roles",
			args: []string{"guild", "guild-1", "list-roles"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildLookup != "guild-1" || !strings.Contains(stdout, "admin") {
					t.Fatalf("guildLookup=%q stdout=%q", manager.guildLookup, stdout)
				}
			},
		},
		{
			name: "guild-list-members",
			args: []string{"guild", "guild-1", "list-members"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.listMembersReq.GuildID != "guild-1" || !strings.Contains(stdout, "user-1") {
					t.Fatalf("listMembersReq=%#v stdout=%q", manager.listMembersReq, stdout)
				}
			},
		},
		{
			name: "guild-inspect",
			args: []string{"guild", "guild-1", "inspect"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildLookup != "guild-1" || !strings.Contains(stdout, "Guild") {
					t.Fatalf("guildLookup=%q stdout=%q", manager.guildLookup, stdout)
				}
			},
		},
		{
			name: "channel-list",
			args: []string{"channel", "list", "--guild", "guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildLookup != "guild-1" || !strings.Contains(stdout, "general") {
					t.Fatalf("guildLookup=%q stdout=%q", manager.guildLookup, stdout)
				}
			},
		},
		{
			name: "channel-create",
			args: []string{"channel", "create", "--guild", "guild-1", "--name", "ops", "--type", "text", "--topic", "hello"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.channelCreate.GuildID != "guild-1" || manager.channelCreate.Name != "ops" || manager.channelCreate.Topic != "hello" {
					t.Fatalf("channelCreate=%#v", manager.channelCreate)
				}
			},
		},
		{
			name: "channel-update",
			args: []string{"channel", "update", "chan-1", "--name", "ops-2"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.channelUpdate.ID != "chan-1" || manager.channelUpdate.Name == nil || *manager.channelUpdate.Name != "ops-2" {
					t.Fatalf("channelUpdate=%#v", manager.channelUpdate)
				}
				if manager.channelUpdate.Topic != nil {
					t.Fatalf("channelUpdate=%#v", manager.channelUpdate)
				}
			},
		},
		{
			name: "channel-delete",
			args: []string{"channel", "delete", "chan-1", "--yes"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.channelDelete != "chan-1" || !strings.Contains(stdout, "channel_id") {
					t.Fatalf("channelDelete=%q stdout=%q", manager.channelDelete, stdout)
				}
			},
		},
		{
			name: "channel-perm-set",
			args: []string{"channel", "perm-set", "chan-1", "--role", "role-1", "--allow", "view_channel", "--deny", "send_messages"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.permSet.ChannelID != "chan-1" || manager.permSet.TargetID != "role-1" {
					t.Fatalf("permSet=%#v", manager.permSet)
				}
			},
		},
		{
			name: "channel-perm-remove",
			args: []string{"channel", "perm-remove", "chan-1", "--role", "role-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.permRemove.ChannelID != "chan-1" || manager.permRemove.TargetID != "role-1" {
					t.Fatalf("permRemove=%#v", manager.permRemove)
				}
			},
		},
		{
			name: "role-list",
			args: []string{"role", "list", "--guild", "guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildLookup != "guild-1" || !strings.Contains(stdout, "admin") {
					t.Fatalf("guildLookup=%q stdout=%q", manager.guildLookup, stdout)
				}
			},
		},
		{
			name: "role-create",
			args: []string{"role", "create", "--guild", "guild-1", "--name", "staff", "--color", "#123456", "--hoist", "--mentionable"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.roleCreate.Name != "staff" || manager.roleCreate.Color == nil || *manager.roleCreate.Color == 0 {
					t.Fatalf("roleCreate=%#v", manager.roleCreate)
				}
			},
		},
		{
			name: "role-update",
			args: []string{"role", "update", "role-1", "--guild", "guild-1", "--name", "staff-2", "--color", "#654321"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.roleUpdate.GuildID != "guild-1" || manager.roleUpdate.RoleID != "role-1" || manager.roleUpdate.Name == nil || *manager.roleUpdate.Name != "staff-2" {
					t.Fatalf("roleUpdate=%#v", manager.roleUpdate)
				}
			},
		},
		{
			name: "role-delete",
			args: []string{"role", "delete", "role-1", "--guild", "guild-1", "--yes"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.roleDelete.GuildID != "guild-1" || manager.roleDelete.RoleID != "role-1" {
					t.Fatalf("roleDelete=%#v", manager.roleDelete)
				}
			},
		},
		{
			name: "role-assign",
			args: []string{"role", "assign", "--guild", "guild-1", "--user", "user-1", "--role", "role-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.roleAssign.GuildID != "guild-1" || manager.roleAssign.UserID != "user-1" || manager.roleAssign.RoleID != "role-1" {
					t.Fatalf("roleAssign=%#v", manager.roleAssign)
				}
			},
		},
		{
			name: "role-unassign",
			args: []string{"role", "unassign", "--guild", "guild-1", "--user", "user-1", "--role", "role-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.roleAssign.GuildID != "guild-1" || manager.roleAssign.UserID != "user-1" || manager.roleAssign.RoleID != "role-1" {
					t.Fatalf("roleAssign=%#v", manager.roleAssign)
				}
			},
		},
		{
			name: "member-list",
			args: []string{"member", "list", "--guild", "guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.listMembersReq.GuildID != "guild-1" || !strings.Contains(stdout, "user-1") {
					t.Fatalf("listMembersReq=%#v stdout=%q", manager.listMembersReq, stdout)
				}
			},
		},
		{
			name: "member-get",
			args: []string{"member", "get", "user-1", "--guild", "guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.getMemberReq.GuildID != "guild-1" || manager.getMemberReq.UserID != "user-1" {
					t.Fatalf("getMemberReq=%#v", manager.getMemberReq)
				}
			},
		},
		{
			name: "member-kick",
			args: []string{"member", "kick", "user-1", "--guild", "guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildUserReq.GuildID != "guild-1" || manager.guildUserReq.UserID != "user-1" {
					t.Fatalf("guildUserReq=%#v", manager.guildUserReq)
				}
			},
		},
		{
			name: "member-ban",
			args: []string{"member", "ban", "user-1", "--guild", "guild-1", "--yes"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.guildUserReq.GuildID != "guild-1" || manager.guildUserReq.UserID != "user-1" {
					t.Fatalf("guildUserReq=%#v", manager.guildUserReq)
				}
			},
		},
		{
			name: "message-send",
			args: []string{"message", "send", "--channel", "chan-1", "--text", "hello"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.sendReq.ChannelID != "chan-1" || manager.sendReq.Text != "hello" {
					t.Fatalf("sendReq=%#v", manager.sendReq)
				}
			},
		},
		{
			name: "message-edit",
			args: []string{"message", "edit", "msg-1", "--channel", "chan-1", "--text", "updated"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.editReq.MessageID != "msg-1" || manager.editReq.Text != "updated" {
					t.Fatalf("editReq=%#v", manager.editReq)
				}
			},
		},
		{
			name: "message-delete",
			args: []string{"message", "delete", "msg-1", "--channel", "chan-1", "--yes"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.messageDelete.MessageID != "msg-1" || manager.messageDelete.ChannelID != "chan-1" {
					t.Fatalf("messageDelete=%#v", manager.messageDelete)
				}
			},
		},
		{
			name: "message-bulk-delete",
			args: []string{"message", "bulk-delete", "--channel", "chan-1", "--user", "user-1", "--yes"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.bulkDeleteReq.ChannelID != "chan-1" || manager.bulkDeleteReq.UserID != "user-1" {
					t.Fatalf("bulkDeleteReq=%#v", manager.bulkDeleteReq)
				}
			},
		},
		{
			name: "message-react",
			args: []string{"message", "react", "msg-1", "--channel", "chan-1", "--emoji", "👍"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.reactReq.MessageID != "msg-1" || manager.reactReq.Emoji != "👍" {
					t.Fatalf("reactReq=%#v", manager.reactReq)
				}
			},
		},
		{
			name: "embed-post",
			args: []string{"embed", "post", "--channel", "chan-1", "--title", "Hello", "--desc", "Body", "--field", "a:b"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.embedReq.ChannelID != "chan-1" || len(manager.embedReq.Fields) != 1 {
					t.Fatalf("embedReq=%#v", manager.embedReq)
				}
			},
		},
		{
			name: "embed-add-button",
			args: []string{"embed", "add-button", "--message", "msg-1", "--channel", "chan-1", "--label", "Go", "--custom-id", "go-1", "--style", "primary"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.buttonReq.MessageID != "msg-1" || manager.buttonReq.CustomID != "go-1" {
					t.Fatalf("buttonReq=%#v", manager.buttonReq)
				}
			},
		},
		{
			name: "embed-add-select",
			args: []string{"embed", "add-select", "--message", "msg-1", "--channel", "chan-1", "--options", "one:1,two:2"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.selectReq.MessageID != "msg-1" || len(manager.selectReq.Options) != 2 {
					t.Fatalf("selectReq=%#v", manager.selectReq)
				}
			},
		},
		{
			name: "interactions-respond",
			args: []string{"interactions", "respond", "--id", "int-1", "--token", "tok-1", "--type", "4", "--body", "{\"content\":\"hi\"}"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.interactionReq.InteractionID != "int-1" || manager.interactionReq.Type != 4 {
					t.Fatalf("interactionReq=%#v", manager.interactionReq)
				}
			},
		},
		{
			name: "rest",
			args: []string{"rest", "GET", "/guilds/guild-1"},
			check: func(t *testing.T, manager *fakeManager, stdout string) {
				t.Helper()
				if manager.restReq.Method != "GET" || manager.restReq.Path != "/guilds/guild-1" {
					t.Fatalf("restReq=%#v", manager.restReq)
				}
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := &fakeManager{
				guild: discord.GuildInfo{ID: "guild-1", Name: "Guild"},
			}
			env, stdout, stderr := newTestEnvironment(manager, nil, "")

			cmd := NewCommand(env)
			cmd.SetArgs(test.args)
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v stderr=%q", err, stderr.String())
			}

			test.check(t, manager, stdout.String())
		})
	}
}

func TestManageCommandRequiresYesForDestructiveActions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "channel-delete", args: []string{"channel", "delete", "chan-1"}},
		{name: "role-delete", args: []string{"role", "delete", "role-1", "--guild", "guild-1"}},
		{name: "member-ban", args: []string{"member", "ban", "user-1", "--guild", "guild-1"}},
		{name: "message-delete", args: []string{"message", "delete", "msg-1", "--channel", "chan-1"}},
		{name: "message-bulk-delete", args: []string{"message", "bulk-delete", "--channel", "chan-1", "--user", "user-1"}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := &fakeManager{}
			env, _, _ := newTestEnvironment(manager, nil, "")
			cmd := NewCommand(env)
			cmd.SetArgs(test.args)

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "--yes") {
				t.Fatalf("Execute() error = %v", err)
			}
		})
	}
}

func TestInteractionsListenCommandStreamsEvents(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{}
	ctx, cancel := context.WithCancel(context.Background())
	manager.onSubscribe = func(handler discord.InteractionHandler) {
		go func() {
			handler(ctx, discord.InteractionEvent{ID: "evt-1", Type: 2})
			cancel()
		}()
	}

	env, stdout, _ := newTestEnvironment(manager, func() context.Context { return ctx }, "")
	cmd := NewCommand(env)
	cmd.SetArgs([]string{"interactions", "listen"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if manager.openCount != 1 || manager.closeCount != 1 {
		t.Fatalf("open=%d close=%d", manager.openCount, manager.closeCount)
	}
	if !strings.Contains(stdout.String(), "evt-1") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestScaffoldCommandPlansAndRequiresYesForApply(t *testing.T) {
	t.Parallel()

	specPath := writeScaffoldSpec(t, `
guild:
  id: guild-1
roles:
  - name: admin
channels:
  - name: general
    type: text
`)

	manager := &fakeManager{
		roles:    []discord.GuildRole{},
		channels: []discord.GuildChannel{},
	}

	env, stdout, _ := newTestEnvironment(manager, nil, "")
	cmd := NewCommand(env)
	cmd.SetArgs([]string{"scaffold", "--from", specPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute(plan) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "\"changes\"") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	env, _, _ = newTestEnvironment(manager, nil, "")
	cmd = NewCommand(env)
	cmd.SetArgs([]string{"scaffold", "--from", specPath, "--apply"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("Execute(apply) error = %v", err)
	}
}

func TestExecCommandRunsYaegiScript(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{}
	env, stdout, _ := newTestEnvironment(manager, nil, "")

	cmd := NewCommand(env)
	cmd.SetArgs([]string{"exec", "--expr", `return client.DeleteChannel("chan-99")`})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if manager.channelDelete != "chan-99" {
		t.Fatalf("channelDelete = %q", manager.channelDelete)
	}
	if !strings.Contains(stdout.String(), "\"ok\": true") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// --- Helpers ---

func newTestEnvironment(manager *fakeManager, contextFunc func() context.Context, stdin string) (Environment, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if contextFunc == nil {
		contextFunc = context.Background
	}

	return Environment{
		Stdin:  strings.NewReader(stdin),
		Stdout: stdout,
		Stderr: stderr,
		ResolveConfig: func() (config.ResolvedConfig, error) {
			return config.ResolvedConfig{}, nil
		},
		CommandContext: contextFunc,
		NewManager: func(config.ResolvedConfig) (discord.Manager, error) {
			return manager, nil
		},
	}, stdout, stderr
}

func writeScaffoldSpec(t *testing.T, content string) string {
	t.Helper()

	path := t.TempDir() + "/scaffold.yaml"
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

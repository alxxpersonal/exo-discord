package scaffold

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeManager struct {
	roles          []discord.GuildRole
	channels       []discord.GuildChannel
	roleCreates    []discord.RoleCreateRequest
	roleUpdates    []discord.RoleUpdateRequest
	roleDeletes    []discord.RoleDeleteRequest
	channelCreates []discord.ChannelCreateRequest
	channelUpdates []discord.ChannelUpdateRequest
	channelDeletes []string
	permSets       []discord.ChannelPermissionSetRequest
	permRemoves    []discord.ChannelPermissionRemoveRequest
}

func (f *fakeManager) Open(context.Context) error  { return nil }
func (f *fakeManager) Close(context.Context) error { return nil }
func (f *fakeManager) Mode() string                { return "bot" }
func (f *fakeManager) SubscribeInteractions(discord.InteractionHandler) func() {
	return func() {}
}
func (f *fakeManager) GetGuild(context.Context, string) (discord.GuildInfo, error) {
	return discord.GuildInfo{}, nil
}
func (f *fakeManager) ListChannels(context.Context, string) ([]discord.GuildChannel, error) {
	return f.channels, nil
}
func (f *fakeManager) ListRoles(context.Context, string) ([]discord.GuildRole, error) {
	return f.roles, nil
}
func (f *fakeManager) ListMembers(context.Context, discord.ListMembersRequest) ([]discord.GuildMember, error) {
	return nil, nil
}
func (f *fakeManager) GetMember(context.Context, discord.GetMemberRequest) (discord.GuildMember, error) {
	return discord.GuildMember{}, nil
}
func (f *fakeManager) CreateChannel(_ context.Context, req discord.ChannelCreateRequest) (discord.GuildChannel, error) {
	f.channelCreates = append(f.channelCreates, req)
	channel := discord.GuildChannel{ID: "created-" + req.Name, GuildID: req.GuildID, Name: req.Name, Type: req.Type, ParentID: req.ParentID, Topic: req.Topic}
	f.channels = append(f.channels, channel)
	return channel, nil
}
func (f *fakeManager) UpdateChannel(_ context.Context, req discord.ChannelUpdateRequest) (discord.GuildChannel, error) {
	f.channelUpdates = append(f.channelUpdates, req)
	return discord.GuildChannel{ID: req.ID, Name: req.ID, Type: "text"}, nil
}
func (f *fakeManager) DeleteChannel(_ context.Context, channelID string) error {
	f.channelDeletes = append(f.channelDeletes, channelID)
	return nil
}
func (f *fakeManager) SetChannelPermission(_ context.Context, req discord.ChannelPermissionSetRequest) error {
	f.permSets = append(f.permSets, req)
	return nil
}
func (f *fakeManager) RemoveChannelPermission(_ context.Context, req discord.ChannelPermissionRemoveRequest) error {
	f.permRemoves = append(f.permRemoves, req)
	return nil
}
func (f *fakeManager) CreateRole(_ context.Context, req discord.RoleCreateRequest) (discord.GuildRole, error) {
	f.roleCreates = append(f.roleCreates, req)
	role := discord.GuildRole{ID: "created-" + req.Name, GuildID: req.GuildID, Name: req.Name}
	f.roles = append(f.roles, role)
	return role, nil
}
func (f *fakeManager) UpdateRole(_ context.Context, req discord.RoleUpdateRequest) (discord.GuildRole, error) {
	f.roleUpdates = append(f.roleUpdates, req)
	return discord.GuildRole{ID: req.RoleID, Name: req.RoleID}, nil
}
func (f *fakeManager) DeleteRole(_ context.Context, req discord.RoleDeleteRequest) error {
	f.roleDeletes = append(f.roleDeletes, req)
	return nil
}
func (f *fakeManager) AssignRole(context.Context, discord.RoleAssignmentRequest) error { return nil }
func (f *fakeManager) UnassignRole(context.Context, discord.RoleAssignmentRequest) error {
	return nil
}
func (f *fakeManager) KickMember(context.Context, discord.GuildUserRequest) error { return nil }
func (f *fakeManager) BanMember(context.Context, discord.GuildUserRequest) error  { return nil }
func (f *fakeManager) SendManagedMessage(context.Context, discord.SendRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, nil
}
func (f *fakeManager) EditManagedMessage(context.Context, discord.EditRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, nil
}
func (f *fakeManager) DeleteManagedMessage(context.Context, discord.MessageTarget) error { return nil }
func (f *fakeManager) BulkDeleteMessages(context.Context, discord.BulkDeleteRequest) (discord.BulkDeleteResult, error) {
	return discord.BulkDeleteResult{}, nil
}
func (f *fakeManager) ReactToManagedMessage(context.Context, discord.ReactRequest) error { return nil }
func (f *fakeManager) PostEmbed(context.Context, discord.EmbedPostRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, nil
}
func (f *fakeManager) AddButton(context.Context, discord.ButtonAddRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, nil
}
func (f *fakeManager) AddSelect(context.Context, discord.SelectAddRequest) (discord.SentMessage, error) {
	return discord.SentMessage{}, nil
}
func (f *fakeManager) RespondInteraction(context.Context, discord.InteractionResponseRequest) error {
	return nil
}
func (f *fakeManager) REST(context.Context, discord.RESTRequest) (discord.RESTResponse, error) {
	return discord.RESTResponse{}, nil
}

// --- Tests ---

func TestDiffMatchesGoldenPlan(t *testing.T) {
	t.Parallel()

	spec, err := LoadSpec(filepath.Join("testdata", "sample.yaml"))
	if err != nil {
		t.Fatalf("LoadSpec() error = %v", err)
	}

	plan := Diff(spec, State{
		GuildID: "guild-1",
		Roles: []discord.GuildRole{
			{ID: "role-1", Name: "admin", Color: 0, Hoist: false, Mentionable: false},
			{ID: "role-2", Name: "moderator", Color: 0, Hoist: false, Mentionable: false},
		},
		Channels: []discord.GuildChannel{
			{ID: "chan-1", Name: "general", Type: "text", Topic: "old"},
			{ID: "chan-2", Name: "obsolete", Type: "text", Topic: ""},
		},
	})

	got, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	got = append(got, '\n')

	want, err := os.ReadFile(filepath.Join("testdata", "sample.golden.json"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(got) != string(want) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestBuildPlanLoadsState(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{
		roles:    []discord.GuildRole{{ID: "role-1", Name: "admin"}},
		channels: []discord.GuildChannel{{ID: "chan-1", Name: "general", Type: "text"}},
	}

	plan, err := BuildPlan(context.Background(), manager, Spec{
		Guild: GuildSpec{ID: "guild-1"},
	})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if plan.GuildID != "guild-1" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestApplyUsesManagerMutations(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{
		roles: []discord.GuildRole{
			{ID: "role-1", Name: "admin", Color: 0},
			{ID: "role-2", Name: "obsolete"},
		},
		channels: []discord.GuildChannel{
			{ID: "cat-1", Name: "old-cat", Type: "category"},
			{ID: "chan-1", Name: "obsolete", Type: "text"},
		},
	}

	spec := Spec{
		Guild: GuildSpec{ID: "guild-1"},
		Roles: []RoleSpec{
			{Name: "admin", Color: "#112233"},
			{Name: "staff"},
		},
		Categories: []ChannelSpec{
			{Name: "team", Type: "category"},
		},
		Channels: []ChannelSpec{
			{
				Name:   "general",
				Type:   "text",
				Parent: "team",
				Overwrites: []OverwriteSpec{
					{Role: "admin", Allow: []string{"view_channel"}},
				},
			},
		},
	}

	plan, err := Apply(context.Background(), manager, spec)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if plan.GuildID != "guild-1" {
		t.Fatalf("plan = %#v", plan)
	}
	if len(manager.roleCreates) != 1 || manager.roleCreates[0].Name != "staff" {
		t.Fatalf("roleCreates = %#v", manager.roleCreates)
	}
	if len(manager.roleUpdates) == 0 || manager.roleUpdates[0].RoleID != "role-1" {
		t.Fatalf("roleUpdates = %#v", manager.roleUpdates)
	}
	if len(manager.channelCreates) < 2 {
		t.Fatalf("channelCreates = %#v", manager.channelCreates)
	}
	if len(manager.permSets) != 1 || manager.permSets[0].TargetID != "role-1" {
		t.Fatalf("permSets = %#v", manager.permSets)
	}
	if len(manager.channelDeletes) == 0 {
		t.Fatalf("channelDeletes = %#v", manager.channelDeletes)
	}
	if len(manager.roleDeletes) == 0 || manager.roleDeletes[0].RoleID != "role-2" {
		t.Fatalf("roleDeletes = %#v", manager.roleDeletes)
	}
}

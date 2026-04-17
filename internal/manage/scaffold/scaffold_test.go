package scaffold

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeManager struct {
	roleListCalls         int
	channelListCalls      int
	roles                 []discord.GuildRole
	channels              []discord.GuildChannel
	roleCreates           []discord.RoleCreateRequest
	roleUpdates           []discord.RoleUpdateRequest
	roleDeletes           []discord.RoleDeleteRequest
	channelCreates        []discord.ChannelCreateRequest
	channelUpdates        []discord.ChannelUpdateRequest
	channelDeletes        []string
	permSets              []discord.ChannelPermissionSetRequest
	permRemoves           []discord.ChannelPermissionRemoveRequest
	failCreateChannelName string
	failDeleteRoleID      string
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
	f.channelListCalls++
	return f.channels, nil
}
func (f *fakeManager) ListRoles(context.Context, string) ([]discord.GuildRole, error) {
	f.roleListCalls++
	return f.roles, nil
}
func (f *fakeManager) ListMembers(context.Context, discord.ListMembersRequest) ([]discord.GuildMember, error) {
	return nil, nil
}
func (f *fakeManager) GetMember(context.Context, discord.GetMemberRequest) (discord.GuildMember, error) {
	return discord.GuildMember{}, nil
}
func (f *fakeManager) CreateChannel(_ context.Context, req discord.ChannelCreateRequest) (discord.GuildChannel, error) {
	if req.Name == f.failCreateChannelName {
		return discord.GuildChannel{}, errors.New("create channel failed")
	}
	f.channelCreates = append(f.channelCreates, req)
	channel := discord.GuildChannel{ID: "created-" + req.Name, GuildID: req.GuildID, Name: req.Name, Type: req.Type, ParentID: req.ParentID, Topic: req.Topic}
	f.channels = append(f.channels, channel)
	return channel, nil
}
func (f *fakeManager) UpdateChannel(_ context.Context, req discord.ChannelUpdateRequest) (discord.GuildChannel, error) {
	f.channelUpdates = append(f.channelUpdates, req)
	channel := discord.GuildChannel{ID: req.ID, Type: "text"}
	if req.Name != nil {
		channel.Name = *req.Name
	}
	if req.Topic != nil {
		channel.Topic = *req.Topic
	}
	if req.ParentID != nil {
		channel.ParentID = *req.ParentID
	}
	return channel, nil
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
	role := discord.GuildRole{ID: req.RoleID, GuildID: req.GuildID}
	if req.Name != nil {
		role.Name = *req.Name
	}
	if req.Color != nil {
		role.Color = *req.Color
	}
	if req.Hoist != nil {
		role.Hoist = *req.Hoist
	}
	if req.Mentionable != nil {
		role.Mentionable = *req.Mentionable
	}
	return role, nil
}
func (f *fakeManager) DeleteRole(_ context.Context, req discord.RoleDeleteRequest) error {
	if req.RoleID == f.failDeleteRoleID {
		return errors.New("delete role failed")
	}
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

	plan, err := Diff(spec, State{
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
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}

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

func TestBuildPlanRejectsInvalidRoleColor(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{}
	_, err := BuildPlan(context.Background(), manager, Spec{
		Guild: GuildSpec{ID: "guild-1"},
		Roles: []RoleSpec{
			{Name: "admin", Color: "#12"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), `parse role color for "admin": invalid color "#12"`) {
		t.Fatalf("BuildPlan() error = %v", err)
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

	plan, err := Apply(context.Background(), manager, spec, ApplyOptions{DeleteExtras: true})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if plan.GuildID != "guild-1" {
		t.Fatalf("plan = %#v", plan)
	}
	if manager.roleListCalls != 1 || manager.channelListCalls != 1 {
		t.Fatalf("load calls = roles:%d channels:%d", manager.roleListCalls, manager.channelListCalls)
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
	if len(plan.Changes) != 7 {
		t.Fatalf("plan.Changes = %#v", plan.Changes)
	}
}

func TestApplyKeepsExtrasWithoutDeleteExtras(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{
		roles: []discord.GuildRole{
			{ID: "role-1", Name: "admin", Color: 0},
			{ID: "role-2", Name: "obsolete"},
		},
		channels: []discord.GuildChannel{
			{ID: "chan-1", Name: "obsolete", Type: "text"},
		},
	}

	spec := Spec{
		Guild: GuildSpec{ID: "guild-1"},
		Roles: []RoleSpec{
			{Name: "admin", Color: "#112233"},
		},
	}

	plan, err := Apply(context.Background(), manager, spec, ApplyOptions{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if len(manager.channelDeletes) != 0 {
		t.Fatalf("channelDeletes = %#v", manager.channelDeletes)
	}
	if len(manager.roleDeletes) != 0 {
		t.Fatalf("roleDeletes = %#v", manager.roleDeletes)
	}
	if len(plan.Warnings) != 2 {
		t.Fatalf("plan.Warnings = %#v", plan.Warnings)
	}
	if plan.Changes[len(plan.Changes)-1].Action == "delete" {
		t.Fatalf("plan.Changes = %#v", plan.Changes)
	}
}

func TestApplyRollbackFailureMatchesGolden(t *testing.T) {
	manager := &fakeManager{
		failCreateChannelName: "general",
		failDeleteRoleID:      "created-staff",
	}

	spec := Spec{
		Guild: GuildSpec{ID: "guild-1"},
		Roles: []RoleSpec{
			{Name: "staff"},
		},
		Channels: []ChannelSpec{
			{Name: "general", Type: "text"},
		},
	}

	plan, err := Apply(context.Background(), manager, spec, ApplyOptions{})
	if err == nil {
		t.Fatal("Apply() error = nil, want failure")
	}

	var applyErr *ApplyError
	if !errors.As(err, &applyErr) {
		t.Fatalf("Apply() error = %T, want *ApplyError", err)
	}

	got, marshalErr := json.MarshalIndent(struct {
		Error         string `json:"error"`
		Applied       Plan   `json:"applied"`
		Rollback      Plan   `json:"rollback"`
		ManualCleanup bool   `json:"manual_cleanup"`
	}{
		Error:         err.Error(),
		Applied:       plan,
		Rollback:      applyErr.Rollback,
		ManualCleanup: applyErr.ManualCleanup,
	}, "", "  ")
	if marshalErr != nil {
		t.Fatalf("MarshalIndent() error = %v", marshalErr)
	}
	got = append(got, '\n')

	want, readErr := os.ReadFile(filepath.Join("testdata", "apply_failure.golden.json"))
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}

	if string(got) != string(want) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestPreparePlanFiltersDeleteChanges(t *testing.T) {
	t.Parallel()

	plan := PreparePlan(Plan{
		GuildID: "guild-1",
		Changes: []Change{
			{Action: "create", Kind: "role", Name: "admin"},
			{Action: "delete", Kind: "channel", Name: "obsolete"},
			{Action: "delete", Kind: "role", Name: "old"},
		},
	}, ApplyOptions{})

	if len(plan.Changes) != 1 || plan.Changes[0].Name != "admin" {
		t.Fatalf("plan.Changes = %#v", plan.Changes)
	}
	if len(plan.Warnings) != 2 {
		t.Fatalf("plan.Warnings = %#v", plan.Warnings)
	}
}

func TestSpecFromStateIncludesSupportedFields(t *testing.T) {
	t.Parallel()

	spec := specFromState(State{
		GuildID: "guild-1",
		Roles: []discord.GuildRole{
			{ID: "role-1", Name: "@everyone"},
			{ID: "role-2", Name: "admin", Color: 0x112233, Hoist: true, Mentionable: true},
		},
		Channels: []discord.GuildChannel{
			{ID: "cat-1", Name: "team", Type: "category"},
			{
				ID:       "chan-1",
				Name:     "general",
				Type:     "text",
				Topic:    "hello",
				ParentID: "cat-1",
				PermissionOverwrites: []discord.PermissionOverwrite{
					{TargetID: "role-2", TargetType: "role", AllowNames: []string{"send_messages", "view_channel"}},
				},
			},
		},
	})

	if len(spec.Roles) != 1 || spec.Roles[0].Color != "#112233" {
		t.Fatalf("spec.Roles = %#v", spec.Roles)
	}
	if len(spec.Categories) != 1 || spec.Categories[0].Name != "team" {
		t.Fatalf("spec.Categories = %#v", spec.Categories)
	}
	if len(spec.Channels) != 1 || spec.Channels[0].Parent != "team" || len(spec.Channels[0].Overwrites) != 1 {
		t.Fatalf("spec.Channels = %#v", spec.Channels)
	}
}

func TestApplyErrorFormatting(t *testing.T) {
	t.Parallel()

	successfulRollback := (&ApplyError{
		Cause: errors.New("apply failed"),
		Applied: Plan{
			Changes: []Change{{Action: "create", Kind: "role", Name: "admin"}},
		},
		Rollback: Plan{
			Changes: []Change{{Action: "delete", Kind: "role", Name: "admin"}},
		},
	}).Error()
	if !strings.Contains(successfulRollback, "rollback succeeded") {
		t.Fatalf("ApplyError() = %q", successfulRollback)
	}

	failedRollback := (&ApplyError{
		Cause:         errors.New("apply failed"),
		Applied:       Plan{Changes: []Change{{Action: "create", Kind: "role", Name: "admin"}}},
		RollbackErr:   errors.New("rollback failed"),
		ManualCleanup: true,
	}).Error()
	if !strings.Contains(failedRollback, "manual cleanup needed") {
		t.Fatalf("ApplyError() = %q", failedRollback)
	}
}

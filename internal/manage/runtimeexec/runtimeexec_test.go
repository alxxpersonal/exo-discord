package runtimeexec

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Test Doubles ---

type fakeManager struct {
	guildID        string
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
}

func (f *fakeManager) Open(context.Context) error  { return nil }
func (f *fakeManager) Close(context.Context) error { return nil }
func (f *fakeManager) Mode() string                { return "bot" }
func (f *fakeManager) SubscribeInteractions(discord.InteractionHandler) func() {
	return func() {}
}
func (f *fakeManager) GetGuild(context.Context, string) (discord.GuildInfo, error) {
	return discord.GuildInfo{ID: "guild-1"}, nil
}
func (f *fakeManager) ListChannels(context.Context, string) ([]discord.GuildChannel, error) {
	return []discord.GuildChannel{{ID: "chan-1"}}, nil
}
func (f *fakeManager) ListRoles(context.Context, string) ([]discord.GuildRole, error) {
	return []discord.GuildRole{{ID: "role-1"}}, nil
}
func (f *fakeManager) ListMembers(context.Context, discord.ListMembersRequest) ([]discord.GuildMember, error) {
	return []discord.GuildMember{{UserID: "user-1"}}, nil
}
func (f *fakeManager) GetMember(context.Context, discord.GetMemberRequest) (discord.GuildMember, error) {
	return discord.GuildMember{UserID: "user-1"}, nil
}
func (f *fakeManager) CreateChannel(_ context.Context, req discord.ChannelCreateRequest) (discord.GuildChannel, error) {
	f.channelCreate = req
	return discord.GuildChannel{ID: "chan-1"}, nil
}
func (f *fakeManager) UpdateChannel(_ context.Context, req discord.ChannelUpdateRequest) (discord.GuildChannel, error) {
	f.channelUpdate = req
	return discord.GuildChannel{ID: req.ID}, nil
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
	return discord.GuildRole{ID: "role-1"}, nil
}
func (f *fakeManager) UpdateRole(_ context.Context, req discord.RoleUpdateRequest) (discord.GuildRole, error) {
	f.roleUpdate = req
	return discord.GuildRole{ID: req.RoleID}, nil
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
	return discord.SentMessage{ChannelID: req.ChannelID, MessageID: "msg-1"}, nil
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
	return discord.BulkDeleteResult{Deleted: 1}, nil
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
	return discord.RESTResponse{Method: req.Method, Path: req.Path, Status: 200}, nil
}

// --- Tests ---

func TestExecuteSourceRunsManagerClientScript(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{}
	source := `package main

func Run(client *ManagerClient) error {
	return client.DeleteChannel("chan-1")
}
`

	if err := ExecuteSource(context.Background(), manager, source); err != nil {
		t.Fatalf("ExecuteSource() error = %v", err)
	}
	if manager.channelDelete != "chan-1" {
		t.Fatalf("channelDelete = %q", manager.channelDelete)
	}
}

func TestExecuteSourceRejectsDisallowedImport(t *testing.T) {
	source := `package main

import "os"

func Run(client *ManagerClient) error {
	return nil
}
`

	err := ExecuteSource(context.Background(), &fakeManager{}, source)
	if err == nil || !strings.Contains(err.Error(), `script import "os" is not allowed`) {
		t.Fatalf("ExecuteSource() error = %v", err)
	}
}

func TestExecuteSourceRejectsNonMainPackage(t *testing.T) {
	source := `package tools

func Run(client *ManagerClient) error {
	return nil
}
`

	err := ExecuteSource(context.Background(), &fakeManager{}, source)
	if err == nil || !strings.Contains(err.Error(), `script package must be main, got "tools"`) {
		t.Fatalf("ExecuteSource() error = %v", err)
	}
}

func TestExecuteSourceRecoversPanic(t *testing.T) {
	source := `package main

func Run(client *ManagerClient) error {
	panic("boom")
}
`

	err := ExecuteSource(context.Background(), &fakeManager{}, source)
	if err == nil || !strings.Contains(err.Error(), "script panicked: boom") {
		t.Fatalf("ExecuteSource() error = %v", err)
	}
}

func TestExecuteSourceTimesOut(t *testing.T) {
	t.Setenv(execTimeoutEnv, "25ms")

	source := `package main

func Run(client *ManagerClient) error {
	for {
	}
}
`

	start := time.Now()
	err := ExecuteSource(context.Background(), &fakeManager{}, source)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ExecuteSource() error = %v, want context deadline exceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("ExecuteSource() took %v, want under 500ms", elapsed)
	}
}

func TestManagerClientDelegatesToManager(t *testing.T) {
	t.Parallel()

	manager := &fakeManager{}
	client := NewManagerClient(context.Background(), manager)

	if _, err := client.GetGuild("guild-1"); err != nil {
		t.Fatalf("GetGuild() error = %v", err)
	}
	if _, err := client.ListChannels("guild-1"); err != nil {
		t.Fatalf("ListChannels() error = %v", err)
	}
	if _, err := client.ListRoles("guild-1"); err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if _, err := client.ListMembers("guild-1", 10); err != nil {
		t.Fatalf("ListMembers() error = %v", err)
	}
	if _, err := client.GetMember("guild-1", "user-1"); err != nil {
		t.Fatalf("GetMember() error = %v", err)
	}
	if _, err := client.CreateChannel("guild-1", "ops", "text", "", "topic"); err != nil {
		t.Fatalf("CreateChannel() error = %v", err)
	}
	if _, err := client.UpdateChannel("chan-1", stringPointer("ops-2"), nil, nil); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}
	if manager.channelUpdate.Topic != nil {
		t.Fatalf("channelUpdate.Topic = %q, want nil", *manager.channelUpdate.Topic)
	}
	if err := client.SetChannelPermission("chan-1", "role-1", "role", []string{"view_channel"}, nil); err != nil {
		t.Fatalf("SetChannelPermission() error = %v", err)
	}
	if err := client.RemoveChannelPermission("chan-1", "role-1", "role"); err != nil {
		t.Fatalf("RemoveChannelPermission() error = %v", err)
	}
	if _, err := client.CreateRole("guild-1", "staff", 1, true, true); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if _, err := client.UpdateRole("guild-1", "role-1", "staff-2", 2, false, false); err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if err := client.DeleteRole("guild-1", "role-1"); err != nil {
		t.Fatalf("DeleteRole() error = %v", err)
	}
	if err := client.AssignRole("guild-1", "user-1", "role-1"); err != nil {
		t.Fatalf("AssignRole() error = %v", err)
	}
	if err := client.UnassignRole("guild-1", "user-1", "role-1"); err != nil {
		t.Fatalf("UnassignRole() error = %v", err)
	}
	if err := client.KickMember("guild-1", "user-1"); err != nil {
		t.Fatalf("KickMember() error = %v", err)
	}
	if err := client.BanMember("guild-1", "user-1"); err != nil {
		t.Fatalf("BanMember() error = %v", err)
	}
	if _, err := client.SendMessage("chan-1", "hello", nil); err != nil {
		t.Fatalf("SendMessage() error = %v", err)
	}
	if _, err := client.EditMessage("chan-1", "msg-1", "updated"); err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}
	if err := client.DeleteMessage("chan-1", "msg-1"); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}
	if _, err := client.BulkDeleteMessages("chan-1", "user-1", ""); err != nil {
		t.Fatalf("BulkDeleteMessages() error = %v", err)
	}
	if err := client.ReactToMessage("chan-1", "msg-1", "👍"); err != nil {
		t.Fatalf("ReactToMessage() error = %v", err)
	}
	if _, err := client.PostEmbed("chan-1", "title", "body", 3, "", nil); err != nil {
		t.Fatalf("PostEmbed() error = %v", err)
	}
	if _, err := client.AddButton("chan-1", "msg-1", "go", "go-1", "primary"); err != nil {
		t.Fatalf("AddButton() error = %v", err)
	}
	if _, err := client.AddSelect("chan-1", "msg-1", "sel-1", []discord.SelectOption{{Label: "one", Value: "1"}}); err != nil {
		t.Fatalf("AddSelect() error = %v", err)
	}
	if err := client.RespondInteraction("int-1", "tok-1", 4, []byte(`{"content":"hi"}`)); err != nil {
		t.Fatalf("RespondInteraction() error = %v", err)
	}
	if _, err := client.REST("GET", "/guilds/guild-1", nil); err != nil {
		t.Fatalf("REST() error = %v", err)
	}
}

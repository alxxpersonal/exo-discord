package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/bwmarrin/discordgo"
)

// --- Test Cases ---

func TestNewDiscordGoManager(t *testing.T) {
	t.Parallel()

	manager, err := NewDiscordGoManager("secret-token", discordgo.IntentGuilds)
	if err != nil {
		t.Fatalf("NewDiscordGoManager() error = %v", err)
	}
	if !manager.session.StateEnabled {
		t.Fatal("StateEnabled = false, want true")
	}
	if got, want := manager.session.Identify.Intents, discordgo.IntentGuilds; got != want {
		t.Fatalf("Identify.Intents = %v, want %v", got, want)
	}
	if got, want := manager.session.Client.Timeout, 30*time.Second; got != want {
		t.Fatalf("Client.Timeout = %v, want %v", got, want)
	}
}

func TestDiscordGoManagerOperations(t *testing.T) {
	t.Parallel()

	session, err := discordgo.New("Bot secret")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}

	var (
		lastBody map[string]any
		paths    []string
	)

	session.Client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			path := strings.TrimPrefix(req.URL.Path, "/api/v9")
			path = strings.TrimPrefix(path, "/api/v10")
			paths = append(paths, req.Method+" "+path)
			if req.Body != nil {
				var decoded map[string]any
				if err := json.NewDecoder(req.Body).Decode(&decoded); err == nil {
					lastBody = decoded
				}
			}

			switch {
			case req.Method == http.MethodGet && path == "/guilds/guild-1":
				return jsonResponse(http.StatusOK, `{"id":"guild-1","name":"Guild","description":"desc","owner_id":"owner-1","member_count":2}`), nil
			case req.Method == http.MethodGet && path == "/guilds/guild-1/channels":
				return jsonResponse(http.StatusOK, `[
					{"id":"cat-1","guild_id":"guild-1","name":"team","type":4,"position":1},
					{"id":"chan-1","guild_id":"guild-1","name":"general","type":0,"topic":"hello","parent_id":"cat-1","position":2,
					 "permission_overwrites":[{"id":"role-1","type":0,"allow":"1024","deny":"2048"}]}
				]`), nil
			case req.Method == http.MethodGet && path == "/guilds/guild-1/roles":
				return jsonResponse(http.StatusOK, `[
					{"id":"role-1","name":"admin","color":1122867,"hoist":true,"mentionable":true,"position":2,"permissions":"3072"},
					{"id":"role-2","name":"staff","color":0,"hoist":false,"mentionable":false,"position":1,"permissions":"0"}
				]`), nil
			case req.Method == http.MethodGet && path == "/guilds/guild-1/members":
				return jsonResponse(http.StatusOK, `[
					{"user":{"id":"user-1","username":"alice"},"nick":"ali","roles":["role-1"],"joined_at":"2026-04-16T22:00:00Z"}
				]`), nil
			case req.Method == http.MethodGet && path == "/guilds/guild-1/members/user-1":
				return jsonResponse(http.StatusOK, `{"user":{"id":"user-1","username":"alice"},"nick":"ali","roles":["role-1"],"joined_at":"2026-04-16T22:00:00Z"}`), nil
			case req.Method == http.MethodPost && path == "/guilds/guild-1/channels":
				return jsonResponse(http.StatusOK, `{"id":"chan-created","guild_id":"guild-1","name":"ops","type":0}`), nil
			case req.Method == http.MethodPatch && path == "/channels/chan-1":
				return jsonResponse(http.StatusOK, `{"id":"chan-1","guild_id":"guild-1","name":"ops-2","type":0,"topic":"topic-2"}`), nil
			case req.Method == http.MethodDelete && path == "/channels/chan-1":
				return jsonResponse(http.StatusOK, `{}`), nil
			case req.Method == http.MethodPut && path == "/channels/chan-1/permissions/role-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodDelete && path == "/channels/chan-1/permissions/role-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodPost && path == "/guilds/guild-1/roles":
				return jsonResponse(http.StatusOK, `{"id":"role-created","name":"ops","color":1,"hoist":true,"mentionable":true,"position":3,"permissions":"0"}`), nil
			case req.Method == http.MethodGet && path == "/users/@me/guilds":
				return jsonResponse(http.StatusOK, `[{"id":"guild-1","name":"Guild"}]`), nil
			case req.Method == http.MethodPatch && path == "/guilds/guild-1/roles/role-1":
				return jsonResponse(http.StatusOK, `{"id":"role-1","name":"ops-2","color":2,"hoist":false,"mentionable":false,"position":2,"permissions":"0"}`), nil
			case req.Method == http.MethodDelete && path == "/guilds/guild-1/roles/role-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodPut && path == "/guilds/guild-1/members/user-1/roles/role-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodDelete && path == "/guilds/guild-1/members/user-1/roles/role-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodDelete && path == "/guilds/guild-1/members/user-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodPut && path == "/guilds/guild-1/bans/user-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodPost && path == "/channels/chan-1/messages":
				return jsonResponse(http.StatusOK, `{"id":"msg-send","channel_id":"chan-1","content":"hello"}`), nil
			case req.Method == http.MethodPatch && path == "/channels/chan-1/messages/msg-1":
				return jsonResponse(http.StatusOK, `{"id":"msg-1","channel_id":"chan-1","content":"updated"}`), nil
			case req.Method == http.MethodDelete && path == "/channels/chan-1/messages/msg-1":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodGet && path == "/channels/chan-1/messages":
				return jsonResponse(http.StatusOK, `[
					{"id":"m2","channel_id":"chan-1","content":"second","author":{"id":"user-1","username":"alice"},"timestamp":"2026-04-16T22:01:00Z"},
					{"id":"m1","channel_id":"chan-1","content":"first","author":{"id":"user-1","username":"alice"},"timestamp":"2026-04-16T22:00:00Z"}
				]`), nil
			case req.Method == http.MethodPost && path == "/channels/chan-1/messages/bulk-delete":
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodPut && strings.Contains(path, "/reactions/"):
				return jsonResponse(http.StatusNoContent, ``), nil
			case req.Method == http.MethodGet && path == "/channels/chan-1/messages/msg-1":
				return jsonResponse(http.StatusOK, `{"id":"msg-1","channel_id":"chan-1","content":"hello","components":[],"author":{"id":"bot-1","username":"bot"}}`), nil
			case req.Method == http.MethodPost && path == "/interactions/int-1/tok-1/callback":
				return jsonResponse(http.StatusNoContent, ``), nil
			default:
				return jsonResponse(http.StatusInternalServerError, `{"message":"unexpected request `+req.Method+` `+path+`"}`), nil
			}
		}),
	}

	manager := &DiscordGoManager{
		session:     session,
		subscribers: make(map[uint64]discord.InteractionHandler),
	}

	info, err := manager.GetGuild(context.Background(), "guild-1")
	if err != nil {
		t.Fatalf("GetGuild() error = %v", err)
	}
	if info.ChannelCount != 2 || info.RoleCount != 2 {
		t.Fatalf("GetGuild() = %#v", info)
	}

	channels, err := manager.ListChannels(context.Background(), "guild-1")
	if err != nil || len(channels) != 2 {
		t.Fatalf("ListChannels() channels=%#v err=%v", channels, err)
	}

	roles, err := manager.ListRoles(context.Background(), "guild-1")
	if err != nil || len(roles) != 2 {
		t.Fatalf("ListRoles() roles=%#v err=%v", roles, err)
	}

	members, err := manager.ListMembers(context.Background(), discord.ListMembersRequest{GuildID: "guild-1", Limit: 10})
	if err != nil || len(members) != 1 {
		t.Fatalf("ListMembers() members=%#v err=%v", members, err)
	}

	member, err := manager.GetMember(context.Background(), discord.GetMemberRequest{GuildID: "guild-1", UserID: "user-1"})
	if err != nil || member.UserID != "user-1" {
		t.Fatalf("GetMember() member=%#v err=%v", member, err)
	}

	if _, err := manager.CreateChannel(context.Background(), discord.ChannelCreateRequest{GuildID: "guild-1", Name: "ops", Type: "text"}); err != nil {
		t.Fatalf("CreateChannel() error = %v", err)
	}
	if _, err := manager.UpdateChannel(context.Background(), discord.ChannelUpdateRequest{ID: "chan-1", Name: stringPointer("ops-2"), Topic: stringPointer("topic-2")}); err != nil {
		t.Fatalf("UpdateChannel() error = %v", err)
	}
	if err := manager.DeleteChannel(context.Background(), "chan-1"); err != nil {
		t.Fatalf("DeleteChannel() error = %v", err)
	}
	if err := manager.SetChannelPermission(context.Background(), discord.ChannelPermissionSetRequest{ChannelID: "chan-1", TargetID: "role-1", TargetType: "role", Allow: []string{"view_channel"}, Deny: []string{"send_messages"}}); err != nil {
		t.Fatalf("SetChannelPermission() error = %v", err)
	}
	if err := manager.RemoveChannelPermission(context.Background(), discord.ChannelPermissionRemoveRequest{ChannelID: "chan-1", TargetID: "role-1", TargetType: "role"}); err != nil {
		t.Fatalf("RemoveChannelPermission() error = %v", err)
	}
	if _, err := manager.CreateRole(context.Background(), discord.RoleCreateRequest{GuildID: "guild-1", Name: "ops", Color: intPointer(1), Hoist: boolPointer(true), Mentionable: boolPointer(true)}); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if _, err := manager.UpdateRole(context.Background(), discord.RoleUpdateRequest{GuildID: "guild-1", RoleID: "role-1", Name: stringPointer("ops-2"), Color: intPointer(2)}); err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if err := manager.DeleteRole(context.Background(), discord.RoleDeleteRequest{GuildID: "guild-1", RoleID: "role-1"}); err != nil {
		t.Fatalf("DeleteRole() error = %v", err)
	}
	if err := manager.AssignRole(context.Background(), discord.RoleAssignmentRequest{GuildID: "guild-1", UserID: "user-1", RoleID: "role-1"}); err != nil {
		t.Fatalf("AssignRole() error = %v", err)
	}
	if err := manager.UnassignRole(context.Background(), discord.RoleAssignmentRequest{GuildID: "guild-1", UserID: "user-1", RoleID: "role-1"}); err != nil {
		t.Fatalf("UnassignRole() error = %v", err)
	}
	if err := manager.KickMember(context.Background(), discord.GuildUserRequest{GuildID: "guild-1", UserID: "user-1"}); err != nil {
		t.Fatalf("KickMember() error = %v", err)
	}
	if err := manager.BanMember(context.Background(), discord.GuildUserRequest{GuildID: "guild-1", UserID: "user-1"}); err != nil {
		t.Fatalf("BanMember() error = %v", err)
	}
	if _, err := manager.SendManagedMessage(context.Background(), discord.SendRequest{ChannelID: "chan-1", Text: "hello"}); err != nil {
		t.Fatalf("SendManagedMessage() error = %v", err)
	}
	if _, err := manager.EditManagedMessage(context.Background(), discord.EditRequest{ChannelID: "chan-1", MessageID: "msg-1", Text: "updated"}); err != nil {
		t.Fatalf("EditManagedMessage() error = %v", err)
	}
	if err := manager.DeleteManagedMessage(context.Background(), discord.MessageTarget{ChannelID: "chan-1", MessageID: "msg-1"}); err != nil {
		t.Fatalf("DeleteManagedMessage() error = %v", err)
	}
	bulk, err := manager.BulkDeleteMessages(context.Background(), discord.BulkDeleteRequest{ChannelID: "chan-1", UserID: "user-1"})
	if err != nil || bulk.Deleted != 2 {
		t.Fatalf("BulkDeleteMessages() bulk=%#v err=%v", bulk, err)
	}
	if err := manager.ReactToManagedMessage(context.Background(), discord.ReactRequest{ChannelID: "chan-1", MessageID: "msg-1", Emoji: "👍"}); err != nil {
		t.Fatalf("ReactToManagedMessage() error = %v", err)
	}
	if _, err := manager.PostEmbed(context.Background(), discord.EmbedPostRequest{ChannelID: "chan-1", Title: "Title"}); err != nil {
		t.Fatalf("PostEmbed() error = %v", err)
	}
	if _, err := manager.AddButton(context.Background(), discord.ButtonAddRequest{ChannelID: "chan-1", MessageID: "msg-1", Label: "Go", CustomID: "go-1", Style: "primary"}); err != nil {
		t.Fatalf("AddButton() error = %v", err)
	}
	if _, err := manager.AddSelect(context.Background(), discord.SelectAddRequest{ChannelID: "chan-1", MessageID: "msg-1", CustomID: "sel-1", Options: []discord.SelectOption{{Label: "one", Value: "1"}}}); err != nil {
		t.Fatalf("AddSelect() error = %v", err)
	}
	if err := manager.RespondInteraction(context.Background(), discord.InteractionResponseRequest{InteractionID: "int-1", InteractionToken: "tok-1", Type: 4, Body: []byte(`{"content":"hi"}`)}); err != nil {
		t.Fatalf("RespondInteraction() error = %v", err)
	}
	restResponse, err := manager.REST(context.Background(), discord.RESTRequest{Method: "GET", Path: "/guilds/guild-1"})
	if err != nil || restResponse.Status != http.StatusOK {
		t.Fatalf("REST() response=%#v err=%v", restResponse, err)
	}

	if lastBody == nil {
		t.Fatal("lastBody = nil, want captured request body")
	}
	if !containsPath(paths, "POST /channels/chan-1/messages/bulk-delete") {
		t.Fatalf("paths = %v", paths)
	}
}

func TestDiscordGoManagerHelpersAndSubscription(t *testing.T) {
	t.Parallel()

	manager := &DiscordGoManager{
		subscribers: make(map[uint64]discord.InteractionHandler),
		opened:      true,
	}
	if manager.Mode() != "bot" {
		t.Fatalf("Mode() = %q, want bot", manager.Mode())
	}
	if err := manager.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	received := make(chan discord.InteractionEvent, 1)
	unsubscribe := manager.SubscribeInteractions(func(_ context.Context, event discord.InteractionEvent) {
		received <- event
	})
	manager.handleInteractionCreate(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "int-1",
			AppID:     "app-1",
			Type:      discordgo.InteractionPing,
			GuildID:   "guild-1",
			ChannelID: "chan-1",
			Token:     "tok-1",
		},
	})
	unsubscribe()

	select {
	case event := <-received:
		if event.ID != "int-1" {
			t.Fatalf("event.ID = %q, want int-1", event.ID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for interaction handler")
	}

	if err := (&DiscordGoManager{}).Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	channelType, err := parseChannelType("text")
	if err != nil || channelType != discordgo.ChannelTypeGuildText {
		t.Fatalf("parseChannelType() type=%v err=%v", channelType, err)
	}
	if channelTypeName(discordgo.ChannelTypeGuildCategory) != "category" {
		t.Fatalf("channelTypeName() = %q", channelTypeName(discordgo.ChannelTypeGuildCategory))
	}
	if parseOverwriteType("member") != discordgo.PermissionOverwriteTypeMember {
		t.Fatal("parseOverwriteType() = role, want member")
	}
	if overwriteTypeName(discordgo.PermissionOverwriteTypeRole) != "role" {
		t.Fatal("overwriteTypeName() != role")
	}
	if parseButtonStyle("danger") != discordgo.DangerButton {
		t.Fatal("parseButtonStyle() != danger")
	}
	normalizedPath, err := normalizeRESTPath("/guilds/guild-1")
	if err != nil || normalizedPath != discordgo.EndpointAPI+"guilds/guild-1" {
		t.Fatalf("normalizeRESTPath() path=%q err=%v", normalizedPath, err)
	}
	bits, err := permissionBitsFromNames([]string{"view_channel", "send_messages"})
	if err != nil || bits == 0 {
		t.Fatalf("permissionBitsFromNames() bits=%d err=%v", bits, err)
	}
	if len(permissionNames(bits)) != 2 {
		t.Fatalf("permissionNames() = %v", permissionNames(bits))
	}
	if normalizePermissionName("View Channel") != "view_channel" {
		t.Fatalf("normalizePermissionName() = %q", normalizePermissionName("View Channel"))
	}
	if len(chunkStrings([]string{"a", "b", "c"}, 2)) != 2 {
		t.Fatalf("chunkStrings() = %v", chunkStrings([]string{"a", "b", "c"}, 2))
	}
	if maxInt(2, 1) != 2 {
		t.Fatalf("maxInt() = %d", maxInt(2, 1))
	}
	if mapRole("guild-1", &discordgo.Role{ID: "role-1", Name: "admin", Color: 1, Permissions: bits}).ColorHex != "#000001" {
		t.Fatalf("mapRole() = %#v", mapRole("guild-1", &discordgo.Role{ID: "role-1", Name: "admin", Color: 1, Permissions: bits}))
	}
	if mapMember("guild-1", &discordgo.Member{
		User:  &discordgo.User{ID: "user-1", Username: "alice"},
		Nick:  "ali",
		Roles: []string{"role-1"},
	}).DisplayName != "ali" {
		t.Fatalf("mapMember() display name mismatch")
	}
	if mapChannel(&discordgo.Channel{
		ID:      "chan-1",
		GuildID: "guild-1",
		Name:    "general",
		Type:    discordgo.ChannelTypeGuildText,
		PermissionOverwrites: []*discordgo.PermissionOverwrite{
			{ID: "role-1", Type: discordgo.PermissionOverwriteTypeRole, Allow: bits},
		},
	}).Type != "text" {
		t.Fatalf("mapChannel() type mismatch")
	}
}

func TestDiscordGoManagerInteractionHandlersUseOpenContextAndCloseWaits(t *testing.T) {
	listenCtx, cancel := context.WithCancel(context.Background())

	manager := &DiscordGoManager{
		subscribers:      make(map[uint64]discord.InteractionHandler),
		interactionCtx:   listenCtx,
		interactionSlots: make(chan struct{}, interactionWorkerLimit),
	}

	started := make(chan struct{}, 1)
	finished := make(chan struct{}, 1)
	manager.SubscribeInteractions(func(ctx context.Context, event discord.InteractionEvent) {
		started <- struct{}{}
		<-ctx.Done()
		finished <- struct{}{}
	})

	manager.handleInteractionCreate(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID:        "int-ctx",
			AppID:     "app-1",
			Type:      discordgo.InteractionPing,
			GuildID:   "guild-1",
			ChannelID: "chan-1",
			Token:     "tok-1",
		},
	})

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for handler start")
	}

	cancel()
	if err := manager.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-finished:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for handler finish")
	}
}

func TestDiscordGoManagerRoleOperationsRequireGuildID(t *testing.T) {
	t.Parallel()

	manager := &DiscordGoManager{}

	tests := []struct {
		name string
		run  func() error
	}{
		{
			name: "update",
			run: func() error {
				_, err := manager.UpdateRole(context.Background(), discord.RoleUpdateRequest{RoleID: "role-1"})
				return err
			},
		},
		{
			name: "delete",
			run: func() error {
				return manager.DeleteRole(context.Background(), discord.RoleDeleteRequest{RoleID: "role-1"})
			},
		},
		{
			name: "assign",
			run: func() error {
				return manager.AssignRole(context.Background(), discord.RoleAssignmentRequest{UserID: "user-1", RoleID: "role-1"})
			},
		},
		{
			name: "unassign",
			run: func() error {
				return manager.UnassignRole(context.Background(), discord.RoleAssignmentRequest{UserID: "user-1", RoleID: "role-1"})
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := test.run()
			if err == nil || !strings.Contains(err.Error(), `requires guild_id for role "role-1"`) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDiscordGoManagerLogsDroppedInteractionEvents(t *testing.T) {
	manager := &DiscordGoManager{
		subscribers:      make(map[uint64]discord.InteractionHandler),
		interactionCtx:   context.Background(),
		interactionSlots: make(chan struct{}, 1),
	}

	release := make(chan struct{})
	manager.SubscribeInteractions(func(context.Context, discord.InteractionEvent) {
		<-release
	})

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = writePipe
	t.Cleanup(func() {
		os.Stderr = originalStderr
		_ = readPipe.Close()
		_ = writePipe.Close()
	})

	manager.handleInteractionCreate(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID: "int-1",
		},
	})
	manager.handleInteractionCreate(&discordgo.InteractionCreate{
		Interaction: &discordgo.Interaction{
			ID: "int-2",
		},
	})

	close(release)
	manager.waitForInteractionHandlers()

	if err := writePipe.Close(); err != nil {
		t.Fatalf("Close(writePipe) error = %v", err)
	}
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if !strings.Contains(string(output), `dropped interaction event "int-2"`) {
		t.Fatalf("stderr = %q", string(output))
	}
}

func TestDiscordGoManagerBulkDeleteMessagesReturnsPartialProgress(t *testing.T) {
	t.Parallel()

	session, err := discordgo.New("Bot secret")
	if err != nil {
		t.Fatalf("discordgo.New() error = %v", err)
	}

	var (
		messagePage     int
		bulkDeleteCalls int
	)
	session.Client = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			path := strings.TrimPrefix(req.URL.Path, "/api/v9")
			path = strings.TrimPrefix(path, "/api/v10")

			switch {
			case req.Method == http.MethodGet && path == "/channels/chan-1/messages":
				messagePage++
				return jsonResponse(http.StatusOK, bulkDeletePage(messagePage)), nil
			case req.Method == http.MethodPost && path == "/channels/chan-1/messages/bulk-delete":
				bulkDeleteCalls++
				if bulkDeleteCalls == 2 {
					return jsonResponse(http.StatusBadRequest, `{"message":"message too old"}`), nil
				}
				return jsonResponse(http.StatusNoContent, ``), nil
			default:
				return jsonResponse(http.StatusInternalServerError, `{"message":"unexpected request"}`), nil
			}
		}),
	}

	manager := &DiscordGoManager{session: session}
	result, err := manager.BulkDeleteMessages(context.Background(), discord.BulkDeleteRequest{
		ChannelID: "chan-1",
		UserID:    "user-1",
	})
	if err == nil || !strings.Contains(err.Error(), "after deleting 100 messages") {
		t.Fatalf("BulkDeleteMessages() error = %v", err)
	}
	if result.Deleted != 100 || len(result.MessageIDs) != 100 {
		t.Fatalf("BulkDeleteMessages() result = %#v", result)
	}
}

func TestDiscordGoManagerRESTRejectsDisallowedHost(t *testing.T) {
	t.Parallel()

	manager := &DiscordGoManager{}
	_, err := manager.REST(context.Background(), discord.RESTRequest{
		Method: "POST",
		Path:   "https://evil.example/exfil",
	})
	if err == nil || !strings.Contains(err.Error(), `rest host "evil.example" is not allowed`) {
		t.Fatalf("REST() error = %v", err)
	}
}

func containsPath(paths []string, target string) bool {
	for _, path := range paths {
		if path == target {
			return true
		}
	}
	return false
}

func boolPointer(value bool) *bool {
	return &value
}

func intPointer(value int) *int {
	return &value
}

func stringPointer(value string) *string {
	return &value
}

func bulkDeletePage(page int) string {
	if page > 2 {
		return `[]`
	}

	start := 200
	if page == 2 {
		start = 100
	}

	var builder strings.Builder
	builder.WriteString("[")
	for idx := 0; idx < 100; idx++ {
		if idx > 0 {
			builder.WriteString(",")
		}
		messageID := start - idx
		builder.WriteString(fmt.Sprintf(
			`{"id":"m%d","channel_id":"chan-1","author":{"id":"user-1","username":"alice"},"timestamp":"2026-04-16T22:00:00Z"}`,
			messageID,
		))
	}
	builder.WriteString("]")
	return builder.String()
}

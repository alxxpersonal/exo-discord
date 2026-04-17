package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/bwmarrin/discordgo"
)

// --- Types ---

// DiscordGoManager adapts discordgo to the internal manager interface.
type DiscordGoManager struct {
	session          *discordgo.Session
	mu               sync.RWMutex
	opened           bool
	nextID           uint64
	subscribers      map[uint64]discord.InteractionHandler
	interactionCtx   context.Context
	interactionSlots chan struct{}
	interactionWG    sync.WaitGroup
}

type permissionSpec struct {
	Name string
	Bits int64
}

const interactionWorkerLimit = 32

// --- Constructors ---

// NewDiscordGoManager creates a discordgo-backed manager.
func NewDiscordGoManager(token string, intents discordgo.Intent) (*DiscordGoManager, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord manager session: %w", err)
	}

	session.StateEnabled = true
	session.Identify.Intents = intents
	session.Client = &http.Client{
		Timeout: 30 * time.Second,
	}

	manager := &DiscordGoManager{
		session:          session,
		subscribers:      make(map[uint64]discord.InteractionHandler),
		interactionCtx:   context.Background(),
		interactionSlots: make(chan struct{}, interactionWorkerLimit),
	}
	session.AddHandler(func(_ *discordgo.Session, interaction *discordgo.InteractionCreate) {
		manager.handleInteractionCreate(interaction)
	})

	return manager, nil
}

// --- Lifecycle ---

// Open opens the underlying Discord gateway session.
func (m *DiscordGoManager) Open(ctx context.Context) error {
	m.ensureInteractionDispatcher()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.interactionCtx = ctx
	if m.interactionCtx == nil {
		m.interactionCtx = context.Background()
	}
	if m.opened {
		return nil
	}
	if err := m.session.Open(); err != nil {
		return err
	}
	m.opened = true
	return nil
}

// Close closes the underlying Discord gateway session.
func (m *DiscordGoManager) Close(context.Context) error {
	m.ensureInteractionDispatcher()

	m.mu.Lock()
	if !m.opened {
		m.mu.Unlock()
		m.waitForInteractionHandlers()
		return nil
	}
	err := m.session.Close()
	m.opened = false
	m.interactionCtx = context.Background()
	m.mu.Unlock()

	m.waitForInteractionHandlers()
	if err != nil {
		return err
	}
	return nil
}

// Mode returns the manager mode.
func (m *DiscordGoManager) Mode() string {
	return "bot"
}

// SubscribeInteractions registers an interaction event handler.
func (m *DiscordGoManager) SubscribeInteractions(handler discord.InteractionHandler) func() {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := m.nextID
	m.nextID++
	m.subscribers[id] = handler

	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.subscribers, id)
	}
}

// --- Guild Operations ---

// GetGuild returns normalized guild metadata.
func (m *DiscordGoManager) GetGuild(ctx context.Context, guildID string) (discord.GuildInfo, error) {
	guild, err := m.session.Guild(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildInfo{}, fmt.Errorf("failed to fetch guild: %w", err)
	}

	channels, err := m.ListChannels(ctx, guildID)
	if err != nil {
		return discord.GuildInfo{}, err
	}
	roles, err := m.ListRoles(ctx, guildID)
	if err != nil {
		return discord.GuildInfo{}, err
	}

	return discord.GuildInfo{
		ID:           guild.ID,
		Name:         guild.Name,
		Description:  guild.Description,
		OwnerID:      guild.OwnerID,
		MemberCount:  guild.MemberCount,
		ChannelCount: len(channels),
		RoleCount:    len(roles),
	}, nil
}

// ListChannels returns normalized guild channels.
func (m *DiscordGoManager) ListChannels(ctx context.Context, guildID string) ([]discord.GuildChannel, error) {
	channels, err := m.session.GuildChannels(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to list channels: %w", err)
	}

	result := make([]discord.GuildChannel, 0, len(channels))
	for _, channel := range channels {
		result = append(result, mapChannel(channel))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Type != result[j].Type {
			return result[i].Type < result[j].Type
		}
		if result[i].Position != result[j].Position {
			return result[i].Position < result[j].Position
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// ListRoles returns normalized guild roles.
func (m *DiscordGoManager) ListRoles(ctx context.Context, guildID string) ([]discord.GuildRole, error) {
	roles, err := m.session.GuildRoles(guildID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to list roles: %w", err)
	}

	result := make([]discord.GuildRole, 0, len(roles))
	for _, role := range roles {
		result = append(result, mapRole(guildID, role))
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Position != result[j].Position {
			return result[i].Position > result[j].Position
		}
		return result[i].Name < result[j].Name
	})

	return result, nil
}

// ListMembers returns normalized guild members.
func (m *DiscordGoManager) ListMembers(ctx context.Context, req discord.ListMembersRequest) ([]discord.GuildMember, error) {
	after := ""
	remaining := req.Limit
	result := make([]discord.GuildMember, 0, maxInt(req.Limit, 0))

	for {
		batchLimit := 1000
		if remaining > 0 && remaining < batchLimit {
			batchLimit = remaining
		}

		members, err := m.session.GuildMembers(req.GuildID, after, batchLimit, discordgo.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("failed to list members: %w", err)
		}
		if len(members) == 0 {
			break
		}

		for _, member := range members {
			result = append(result, mapMember(req.GuildID, member))
			after = member.User.ID
		}

		if remaining > 0 {
			remaining -= len(members)
			if remaining <= 0 {
				break
			}
		}

		if len(members) < batchLimit {
			break
		}
	}

	return result, nil
}

// GetMember returns normalized guild member metadata.
func (m *DiscordGoManager) GetMember(ctx context.Context, req discord.GetMemberRequest) (discord.GuildMember, error) {
	member, err := m.session.GuildMember(req.GuildID, req.UserID, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildMember{}, fmt.Errorf("failed to fetch member: %w", err)
	}

	return mapMember(req.GuildID, member), nil
}

// --- Channel Operations ---

// CreateChannel creates a guild channel.
func (m *DiscordGoManager) CreateChannel(ctx context.Context, req discord.ChannelCreateRequest) (discord.GuildChannel, error) {
	channelType, err := parseChannelType(req.Type)
	if err != nil {
		return discord.GuildChannel{}, err
	}

	channel, err := m.session.GuildChannelCreateComplex(req.GuildID, discordgo.GuildChannelCreateData{
		Name:     req.Name,
		Type:     channelType,
		ParentID: req.ParentID,
		Topic:    req.Topic,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildChannel{}, fmt.Errorf("failed to create channel: %w", err)
	}

	return mapChannel(channel), nil
}

// UpdateChannel updates a guild channel.
func (m *DiscordGoManager) UpdateChannel(ctx context.Context, req discord.ChannelUpdateRequest) (discord.GuildChannel, error) {
	body := map[string]any{}
	if req.Name != nil {
		body["name"] = *req.Name
	}
	if req.Topic != nil {
		body["topic"] = *req.Topic
	}
	if req.ParentID != nil {
		body["parent_id"] = *req.ParentID
	}

	payload, err := m.session.Request(http.MethodPatch, discordgo.EndpointChannel(req.ID), body, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildChannel{}, fmt.Errorf("failed to update channel: %w", err)
	}

	var channel discordgo.Channel
	if err := json.Unmarshal(payload, &channel); err != nil {
		return discord.GuildChannel{}, fmt.Errorf("failed to decode updated channel: %w", err)
	}

	return mapChannel(&channel), nil
}

// DeleteChannel deletes a guild channel.
func (m *DiscordGoManager) DeleteChannel(ctx context.Context, channelID string) error {
	if _, err := m.session.ChannelDelete(channelID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to delete channel: %w", err)
	}
	return nil
}

// SetChannelPermission creates or updates a channel overwrite.
func (m *DiscordGoManager) SetChannelPermission(ctx context.Context, req discord.ChannelPermissionSetRequest) error {
	allow, err := permissionBitsFromNames(req.Allow)
	if err != nil {
		return err
	}
	deny, err := permissionBitsFromNames(req.Deny)
	if err != nil {
		return err
	}

	if err := m.session.ChannelPermissionSet(
		req.ChannelID,
		req.TargetID,
		parseOverwriteType(req.TargetType),
		allow,
		deny,
		discordgo.WithContext(ctx),
	); err != nil {
		return fmt.Errorf("failed to set channel permission overwrite: %w", err)
	}

	return nil
}

// RemoveChannelPermission deletes a channel overwrite.
func (m *DiscordGoManager) RemoveChannelPermission(ctx context.Context, req discord.ChannelPermissionRemoveRequest) error {
	if err := m.session.ChannelPermissionDelete(req.ChannelID, req.TargetID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to remove channel permission overwrite: %w", err)
	}
	return nil
}

// --- Role Operations ---

// CreateRole creates a guild role.
func (m *DiscordGoManager) CreateRole(ctx context.Context, req discord.RoleCreateRequest) (discord.GuildRole, error) {
	role, err := m.session.GuildRoleCreate(req.GuildID, &discordgo.RoleParams{
		Name:        req.Name,
		Color:       req.Color,
		Hoist:       req.Hoist,
		Mentionable: req.Mentionable,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildRole{}, fmt.Errorf("failed to create role: %w", err)
	}

	return mapRole(req.GuildID, role), nil
}

// UpdateRole updates a guild role.
func (m *DiscordGoManager) UpdateRole(ctx context.Context, req discord.RoleUpdateRequest) (discord.GuildRole, error) {
	guildID, err := m.resolveRoleGuildID(ctx, req.GuildID, req.RoleID)
	if err != nil {
		return discord.GuildRole{}, err
	}

	params := &discordgo.RoleParams{
		Color:       req.Color,
		Hoist:       req.Hoist,
		Mentionable: req.Mentionable,
	}
	if req.Name != nil {
		params.Name = *req.Name
	}

	role, err := m.session.GuildRoleEdit(guildID, req.RoleID, params, discordgo.WithContext(ctx))
	if err != nil {
		return discord.GuildRole{}, fmt.Errorf("failed to update role: %w", err)
	}

	return mapRole(guildID, role), nil
}

// DeleteRole deletes a guild role.
func (m *DiscordGoManager) DeleteRole(ctx context.Context, req discord.RoleDeleteRequest) error {
	guildID, err := m.resolveRoleGuildID(ctx, req.GuildID, req.RoleID)
	if err != nil {
		return err
	}

	if err := m.session.GuildRoleDelete(guildID, req.RoleID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to delete role: %w", err)
	}
	return nil
}

// AssignRole assigns a role to a guild member.
func (m *DiscordGoManager) AssignRole(ctx context.Context, req discord.RoleAssignmentRequest) error {
	guildID, err := m.resolveRoleGuildID(ctx, req.GuildID, req.RoleID)
	if err != nil {
		return err
	}

	if err := m.session.GuildMemberRoleAdd(guildID, req.UserID, req.RoleID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to assign role: %w", err)
	}
	return nil
}

// UnassignRole removes a role from a guild member.
func (m *DiscordGoManager) UnassignRole(ctx context.Context, req discord.RoleAssignmentRequest) error {
	guildID, err := m.resolveRoleGuildID(ctx, req.GuildID, req.RoleID)
	if err != nil {
		return err
	}

	if err := m.session.GuildMemberRoleRemove(guildID, req.UserID, req.RoleID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to unassign role: %w", err)
	}
	return nil
}

// --- Member Operations ---

// KickMember removes a member from a guild.
func (m *DiscordGoManager) KickMember(ctx context.Context, req discord.GuildUserRequest) error {
	if err := m.session.GuildMemberDelete(req.GuildID, req.UserID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to kick member: %w", err)
	}
	return nil
}

// BanMember bans a member from a guild.
func (m *DiscordGoManager) BanMember(ctx context.Context, req discord.GuildUserRequest) error {
	if err := m.session.GuildBanCreate(req.GuildID, req.UserID, 0, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to ban member: %w", err)
	}
	return nil
}

// --- Message Operations ---

// SendManagedMessage sends a Discord message.
func (m *DiscordGoManager) SendManagedMessage(ctx context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	messageSend, cleanup, err := buildMessageSend(req.Text, req.Files, nil)
	if err != nil {
		return discord.SentMessage{}, err
	}
	defer cleanup()

	sent, err := m.session.ChannelMessageSendComplex(req.ChannelID, messageSend, discordgo.WithContext(ctx))
	if err != nil {
		return discord.SentMessage{}, fmt.Errorf("failed to send message: %w", err)
	}

	return discord.SentMessage{
		ChannelID: sent.ChannelID,
		MessageID: sent.ID,
		Content:   sent.Content,
	}, nil
}

// EditManagedMessage edits a Discord message.
func (m *DiscordGoManager) EditManagedMessage(ctx context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	edited, err := m.session.ChannelMessageEditComplex(
		discordgo.NewMessageEdit(req.ChannelID, req.MessageID).SetContent(req.Text),
		discordgo.WithContext(ctx),
	)
	if err != nil {
		return discord.SentMessage{}, fmt.Errorf("failed to edit message: %w", err)
	}

	return discord.SentMessage{
		ChannelID: edited.ChannelID,
		MessageID: edited.ID,
		Content:   edited.Content,
	}, nil
}

// DeleteManagedMessage deletes a Discord message.
func (m *DiscordGoManager) DeleteManagedMessage(ctx context.Context, target discord.MessageTarget) error {
	if err := m.session.ChannelMessageDelete(target.ChannelID, target.MessageID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}
	return nil
}

// BulkDeleteMessages bulk deletes messages for a given author.
func (m *DiscordGoManager) BulkDeleteMessages(ctx context.Context, req discord.BulkDeleteRequest) (discord.BulkDeleteResult, error) {
	var (
		beforeID   = req.BeforeID
		messageIDs []string
	)

	for {
		messages, err := m.session.ChannelMessages(req.ChannelID, 100, beforeID, "", "", discordgo.WithContext(ctx))
		if err != nil {
			return discord.BulkDeleteResult{}, fmt.Errorf("failed to fetch messages for bulk delete: %w", err)
		}
		if len(messages) == 0 {
			break
		}

		for _, message := range messages {
			if message.Author != nil && message.Author.ID == req.UserID {
				messageIDs = append(messageIDs, message.ID)
			}
		}

		beforeID = messages[len(messages)-1].ID
		if len(messages) < 100 {
			break
		}
	}

	for _, chunk := range chunkStrings(messageIDs, 100) {
		if len(chunk) == 1 {
			if err := m.session.ChannelMessageDelete(req.ChannelID, chunk[0], discordgo.WithContext(ctx)); err != nil {
				return discord.BulkDeleteResult{}, fmt.Errorf("failed to delete message %s: %w", chunk[0], err)
			}
			continue
		}
		if err := m.session.ChannelMessagesBulkDelete(req.ChannelID, chunk, discordgo.WithContext(ctx)); err != nil {
			return discord.BulkDeleteResult{}, fmt.Errorf("failed to bulk delete messages: %w", err)
		}
	}

	return discord.BulkDeleteResult{
		Deleted:    len(messageIDs),
		MessageIDs: messageIDs,
	}, nil
}

// ReactToManagedMessage adds an emoji reaction to a message.
func (m *DiscordGoManager) ReactToManagedMessage(ctx context.Context, req discord.ReactRequest) error {
	if err := m.session.MessageReactionAdd(req.ChannelID, req.MessageID, req.Emoji, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to add reaction: %w", err)
	}
	return nil
}

// --- Embed Operations ---

// PostEmbed sends an embed to a channel.
func (m *DiscordGoManager) PostEmbed(ctx context.Context, req discord.EmbedPostRequest) (discord.SentMessage, error) {
	embed := &discordgo.MessageEmbed{
		Title:       req.Title,
		Description: req.Description,
	}
	if req.Color != nil {
		embed.Color = *req.Color
	}
	if req.Image != "" {
		embed.Image = &discordgo.MessageEmbedImage{
			URL: req.Image,
		}
	}
	if len(req.Fields) > 0 {
		embed.Fields = make([]*discordgo.MessageEmbedField, 0, len(req.Fields))
		for _, field := range req.Fields {
			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name:  field.Name,
				Value: field.Value,
			})
		}
	}

	sent, err := m.session.ChannelMessageSendComplex(req.ChannelID, &discordgo.MessageSend{
		Embeds: []*discordgo.MessageEmbed{embed},
		AllowedMentions: &discordgo.MessageAllowedMentions{
			Parse: []discordgo.AllowedMentionType{},
		},
	}, discordgo.WithContext(ctx))
	if err != nil {
		return discord.SentMessage{}, fmt.Errorf("failed to post embed: %w", err)
	}

	return discord.SentMessage{
		ChannelID: sent.ChannelID,
		MessageID: sent.ID,
		Content:   sent.Content,
	}, nil
}

// AddButton appends a button row to a message.
func (m *DiscordGoManager) AddButton(ctx context.Context, req discord.ButtonAddRequest) (discord.SentMessage, error) {
	edited, err := m.editMessageComponents(ctx, req.ChannelID, req.MessageID, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.Button{
				Label:    req.Label,
				CustomID: req.CustomID,
				Style:    parseButtonStyle(req.Style),
			},
		},
	})
	if err != nil {
		return discord.SentMessage{}, err
	}

	return discord.SentMessage{
		ChannelID: edited.ChannelID,
		MessageID: edited.ID,
		Content:   edited.Content,
	}, nil
}

// AddSelect appends a select row to a message.
func (m *DiscordGoManager) AddSelect(ctx context.Context, req discord.SelectAddRequest) (discord.SentMessage, error) {
	options := make([]discordgo.SelectMenuOption, 0, len(req.Options))
	for _, option := range req.Options {
		options = append(options, discordgo.SelectMenuOption{
			Label: option.Label,
			Value: option.Value,
		})
	}

	edited, err := m.editMessageComponents(ctx, req.ChannelID, req.MessageID, discordgo.ActionsRow{
		Components: []discordgo.MessageComponent{
			discordgo.SelectMenu{
				MenuType: discordgo.StringSelectMenu,
				CustomID: req.CustomID,
				Options:  options,
			},
		},
	})
	if err != nil {
		return discord.SentMessage{}, err
	}

	return discord.SentMessage{
		ChannelID: edited.ChannelID,
		MessageID: edited.ID,
		Content:   edited.Content,
	}, nil
}

// --- Interaction Operations ---

// RespondInteraction sends a raw interaction callback response.
func (m *DiscordGoManager) RespondInteraction(ctx context.Context, req discord.InteractionResponseRequest) error {
	payload := map[string]any{
		"type": req.Type,
	}
	if len(req.Body) > 0 {
		var body any
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return fmt.Errorf("invalid interaction body json: %w", err)
		}
		payload["data"] = body
	}

	if _, err := m.session.Request(
		http.MethodPost,
		discordgo.EndpointInteractionResponse(req.InteractionID, req.InteractionToken),
		payload,
		discordgo.WithContext(ctx),
	); err != nil {
		return fmt.Errorf("failed to respond to interaction: %w", err)
	}

	return nil
}

// --- Raw REST Operations ---

// REST executes a raw Discord REST request.
func (m *DiscordGoManager) REST(ctx context.Context, req discord.RESTRequest) (discord.RESTResponse, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		return discord.RESTResponse{}, fmt.Errorf("rest method must not be empty")
	}

	path := strings.TrimSpace(req.Path)
	if path == "" {
		return discord.RESTResponse{}, fmt.Errorf("rest path must not be empty")
	}

	var body any
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &body); err != nil {
			return discord.RESTResponse{}, fmt.Errorf("invalid rest body json: %w", err)
		}
	}

	url := normalizeRESTPath(path)
	payload, err := m.session.Request(method, url, body, discordgo.WithContext(ctx))
	if err != nil {
		return discord.RESTResponse{}, fmt.Errorf("failed to execute rest request: %w", err)
	}

	status := http.StatusOK
	if len(payload) == 0 {
		status = http.StatusNoContent
	}

	return discord.RESTResponse{
		Method: method,
		Path:   path,
		Status: status,
		Body:   string(payload),
	}, nil
}

// --- Internal Helpers ---

func (m *DiscordGoManager) handleInteractionCreate(interaction *discordgo.InteractionCreate) {
	if interaction == nil || interaction.Interaction == nil {
		return
	}
	m.ensureInteractionDispatcher()

	raw, err := json.Marshal(interaction.Interaction)
	if err != nil {
		raw = nil
	}

	name := ""
	switch interaction.Type {
	case discordgo.InteractionApplicationCommand:
		name = interaction.ApplicationCommandData().Name
	case discordgo.InteractionMessageComponent:
		name = interaction.MessageComponentData().CustomID
	}

	event := discord.InteractionEvent{
		ID:            interaction.ID,
		Type:          int(interaction.Type),
		ApplicationID: interaction.AppID,
		GuildID:       interaction.GuildID,
		ChannelID:     interaction.ChannelID,
		Token:         interaction.Token,
		Name:          name,
		Raw:           raw,
	}

	m.mu.RLock()
	ctx := m.interactionCtx
	handlers := make([]discord.InteractionHandler, 0, len(m.subscribers))
	for _, handler := range m.subscribers {
		handlers = append(handlers, handler)
	}
	m.mu.RUnlock()

	if ctx == nil {
		ctx = context.Background()
	}
	for _, handler := range handlers {
		if !m.dispatchInteractionHandler(ctx, handler, event) {
			fmt.Fprintf(os.Stderr, "dropped interaction event %q because the listener pool is saturated\n", event.ID)
		}
	}
}

func (m *DiscordGoManager) ensureInteractionDispatcher() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.interactionCtx == nil {
		m.interactionCtx = context.Background()
	}
	if m.interactionSlots == nil {
		m.interactionSlots = make(chan struct{}, interactionWorkerLimit)
	}
}

func (m *DiscordGoManager) dispatchInteractionHandler(
	ctx context.Context,
	handler discord.InteractionHandler,
	event discord.InteractionEvent,
) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	select {
	case m.interactionSlots <- struct{}{}:
		m.interactionWG.Add(1)
		go func() {
			defer m.interactionWG.Done()
			defer func() {
				<-m.interactionSlots
			}()
			handler(ctx, event)
		}()
		return true
	default:
		return false
	}
}

func (m *DiscordGoManager) waitForInteractionHandlers() {
	m.interactionWG.Wait()
}

func (m *DiscordGoManager) resolveRoleGuildID(ctx context.Context, guildID string, roleID string) (string, error) {
	if strings.TrimSpace(guildID) != "" {
		return guildID, nil
	}

	after := ""
	for {
		guilds, err := m.session.UserGuilds(200, "", after, false, discordgo.WithContext(ctx))
		if err != nil {
			return "", fmt.Errorf("failed to list guilds for role resolution: %w", err)
		}
		if len(guilds) == 0 {
			break
		}

		for _, guild := range guilds {
			roles, err := m.session.GuildRoles(guild.ID, discordgo.WithContext(ctx))
			if err != nil {
				return "", fmt.Errorf("failed to list roles for guild %s: %w", guild.ID, err)
			}
			for _, role := range roles {
				if role.ID == roleID {
					return guild.ID, nil
				}
			}
		}

		after = guilds[len(guilds)-1].ID
		if len(guilds) < 200 {
			break
		}
	}

	return "", fmt.Errorf("failed to resolve guild for role %s", roleID)
}

func (m *DiscordGoManager) editMessageComponents(
	ctx context.Context,
	channelID string,
	messageID string,
	component discordgo.ActionsRow,
) (*discordgo.Message, error) {
	message, err := m.session.ChannelMessage(channelID, messageID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message for component update: %w", err)
	}

	components := append([]discordgo.MessageComponent(nil), message.Components...)
	components = append(components, component)

	edited, err := m.session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		ID:         messageID,
		Channel:    channelID,
		Components: &components,
	}, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to update message components: %w", err)
	}

	return edited, nil
}

func mapChannel(channel *discordgo.Channel) discord.GuildChannel {
	overwrites := make([]discord.PermissionOverwrite, 0, len(channel.PermissionOverwrites))
	for _, overwrite := range channel.PermissionOverwrites {
		overwrites = append(overwrites, discord.PermissionOverwrite{
			TargetID:   overwrite.ID,
			TargetType: overwriteTypeName(overwrite.Type),
			Allow:      overwrite.Allow,
			Deny:       overwrite.Deny,
			AllowNames: permissionNames(overwrite.Allow),
			DenyNames:  permissionNames(overwrite.Deny),
		})
	}

	return discord.GuildChannel{
		ID:                   channel.ID,
		GuildID:              channel.GuildID,
		Name:                 channel.Name,
		Type:                 channelTypeName(channel.Type),
		Topic:                channel.Topic,
		ParentID:             channel.ParentID,
		Position:             channel.Position,
		NSFW:                 channel.NSFW,
		PermissionOverwrites: overwrites,
	}
}

func mapRole(guildID string, role *discordgo.Role) discord.GuildRole {
	return discord.GuildRole{
		ID:              role.ID,
		GuildID:         guildID,
		Name:            role.Name,
		Color:           role.Color,
		ColorHex:        fmt.Sprintf("#%06x", role.Color),
		Hoist:           role.Hoist,
		Mentionable:     role.Mentionable,
		Position:        role.Position,
		Permissions:     role.Permissions,
		PermissionNames: permissionNames(role.Permissions),
	}
}

func mapMember(guildID string, member *discordgo.Member) discord.GuildMember {
	username := ""
	isBot := false
	if member.User != nil {
		username = member.User.Username
		isBot = member.User.Bot
	}

	displayName := member.Nick
	if displayName == "" {
		displayName = username
	}

	joinedAt := ""
	if !member.JoinedAt.IsZero() {
		joinedAt = member.JoinedAt.UTC().Format(time.RFC3339)
	}

	return discord.GuildMember{
		GuildID:     guildID,
		UserID:      member.User.ID,
		Username:    username,
		DisplayName: displayName,
		Nick:        member.Nick,
		Bot:         isBot,
		Roles:       append([]string(nil), member.Roles...),
		JoinedAt:    joinedAt,
	}
}

func parseChannelType(value string) (discordgo.ChannelType, error) {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "text":
		return discordgo.ChannelTypeGuildText, nil
	case "voice":
		return discordgo.ChannelTypeGuildVoice, nil
	case "category":
		return discordgo.ChannelTypeGuildCategory, nil
	default:
		return discordgo.ChannelTypeGuildText, fmt.Errorf("unsupported channel type %q", value)
	}
}

func channelTypeName(channelType discordgo.ChannelType) string {
	switch channelType {
	case discordgo.ChannelTypeGuildText:
		return "text"
	case discordgo.ChannelTypeGuildVoice:
		return "voice"
	case discordgo.ChannelTypeGuildCategory:
		return "category"
	default:
		return fmt.Sprintf("type_%d", channelType)
	}
}

func parseOverwriteType(value string) discordgo.PermissionOverwriteType {
	if strings.EqualFold(strings.TrimSpace(value), "member") || strings.EqualFold(strings.TrimSpace(value), "user") {
		return discordgo.PermissionOverwriteTypeMember
	}
	return discordgo.PermissionOverwriteTypeRole
}

func overwriteTypeName(value discordgo.PermissionOverwriteType) string {
	if value == discordgo.PermissionOverwriteTypeMember {
		return "member"
	}
	return "role"
}

func parseButtonStyle(value string) discordgo.ButtonStyle {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "secondary":
		return discordgo.SecondaryButton
	case "success":
		return discordgo.SuccessButton
	case "danger":
		return discordgo.DangerButton
	case "link":
		return discordgo.LinkButton
	default:
		return discordgo.PrimaryButton
	}
}

func normalizeRESTPath(path string) string {
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		return path
	}
	return discordgo.EndpointAPI + strings.TrimPrefix(path, "/")
}

func permissionBitsFromNames(names []string) (int64, error) {
	var bits int64
	for _, name := range names {
		normalized := normalizePermissionName(name)
		if normalized == "" {
			continue
		}
		spec, ok := permissionNameMap[normalized]
		if !ok {
			return 0, fmt.Errorf("unsupported permission %q", name)
		}
		bits |= spec
	}
	return bits, nil
}

func permissionNames(bits int64) []string {
	result := make([]string, 0, len(permissionSpecs))
	for _, spec := range permissionSpecs {
		if bits&spec.Bits == spec.Bits {
			result = append(result, spec.Name)
		}
	}
	return result
}

func normalizePermissionName(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		return nil
	}

	chunks := make([][]string, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

// --- Permission Constants ---

var permissionSpecs = []permissionSpec{
	{Name: "create_instant_invite", Bits: discordgo.PermissionCreateInstantInvite},
	{Name: "kick_members", Bits: discordgo.PermissionKickMembers},
	{Name: "ban_members", Bits: discordgo.PermissionBanMembers},
	{Name: "administrator", Bits: discordgo.PermissionAdministrator},
	{Name: "manage_channels", Bits: discordgo.PermissionManageChannels},
	{Name: "manage_guild", Bits: discordgo.PermissionManageGuild},
	{Name: "add_reactions", Bits: discordgo.PermissionAddReactions},
	{Name: "view_audit_log", Bits: discordgo.PermissionViewAuditLogs},
	{Name: "priority_speaker", Bits: discordgo.PermissionVoicePrioritySpeaker},
	{Name: "stream", Bits: discordgo.PermissionVoiceStreamVideo},
	{Name: "view_channel", Bits: discordgo.PermissionViewChannel},
	{Name: "send_messages", Bits: discordgo.PermissionSendMessages},
	{Name: "send_tts_messages", Bits: discordgo.PermissionSendTTSMessages},
	{Name: "manage_messages", Bits: discordgo.PermissionManageMessages},
	{Name: "embed_links", Bits: discordgo.PermissionEmbedLinks},
	{Name: "attach_files", Bits: discordgo.PermissionAttachFiles},
	{Name: "read_message_history", Bits: discordgo.PermissionReadMessageHistory},
	{Name: "mention_everyone", Bits: discordgo.PermissionMentionEveryone},
	{Name: "use_external_emojis", Bits: discordgo.PermissionUseExternalEmojis},
	{Name: "view_guild_insights", Bits: discordgo.PermissionViewGuildInsights},
	{Name: "connect", Bits: discordgo.PermissionVoiceConnect},
	{Name: "speak", Bits: discordgo.PermissionVoiceSpeak},
	{Name: "mute_members", Bits: discordgo.PermissionVoiceMuteMembers},
	{Name: "deafen_members", Bits: discordgo.PermissionVoiceDeafenMembers},
	{Name: "move_members", Bits: discordgo.PermissionVoiceMoveMembers},
	{Name: "use_vad", Bits: discordgo.PermissionVoiceUseVAD},
	{Name: "change_nickname", Bits: discordgo.PermissionChangeNickname},
	{Name: "manage_nicknames", Bits: discordgo.PermissionManageNicknames},
	{Name: "manage_roles", Bits: discordgo.PermissionManageRoles},
	{Name: "manage_webhooks", Bits: discordgo.PermissionManageWebhooks},
	{Name: "manage_guild_expressions", Bits: discordgo.PermissionManageGuildExpressions},
	{Name: "use_application_commands", Bits: discordgo.PermissionUseApplicationCommands},
	{Name: "request_to_speak", Bits: discordgo.PermissionVoiceRequestToSpeak},
	{Name: "manage_events", Bits: discordgo.PermissionManageEvents},
	{Name: "manage_threads", Bits: discordgo.PermissionManageThreads},
	{Name: "create_public_threads", Bits: discordgo.PermissionCreatePublicThreads},
	{Name: "create_private_threads", Bits: discordgo.PermissionCreatePrivateThreads},
	{Name: "use_external_stickers", Bits: discordgo.PermissionUseExternalStickers},
	{Name: "send_messages_in_threads", Bits: discordgo.PermissionSendMessagesInThreads},
	{Name: "use_embedded_activities", Bits: discordgo.PermissionUseEmbeddedActivities},
	{Name: "moderate_members", Bits: discordgo.PermissionModerateMembers},
	{Name: "view_creator_monetization_analytics", Bits: discordgo.PermissionViewCreatorMonetizationAnalytics},
	{Name: "use_soundboard", Bits: discordgo.PermissionUseSoundboard},
	{Name: "create_guild_expressions", Bits: discordgo.PermissionCreateGuildExpressions},
	{Name: "create_events", Bits: discordgo.PermissionCreateEvents},
	{Name: "use_external_sounds", Bits: discordgo.PermissionUseExternalSounds},
	{Name: "send_voice_messages", Bits: discordgo.PermissionSendVoiceMessages},
	{Name: "send_polls", Bits: discordgo.PermissionSendPolls},
	{Name: "use_external_apps", Bits: discordgo.PermissionUseExternalApps},
}

var permissionNameMap = func() map[string]int64 {
	result := make(map[string]int64, len(permissionSpecs)+5)
	for _, spec := range permissionSpecs {
		result[spec.Name] = spec.Bits
	}
	result["read_messages"] = discordgo.PermissionViewChannel
	result["manage_server"] = discordgo.PermissionManageGuild
	result["manage_emojis"] = discordgo.PermissionManageGuildExpressions
	result["use_slash_commands"] = discordgo.PermissionUseApplicationCommands
	result["use_activities"] = discordgo.PermissionUseEmbeddedActivities
	return result
}()

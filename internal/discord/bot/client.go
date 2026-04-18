package bot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/bwmarrin/discordgo"
)

// --- Client Interface ---

// Client defines the Discord gateway and REST operations used by the bot session.
type Client interface {
	Open() error
	Close() error
	BotUserID() string
	OnMessageCreate(func(context.Context, discord.RawMessageEvent)) func()
	SendMessage(context.Context, discord.SendRequest) (discord.SentMessage, error)
	Reply(context.Context, discord.ReplyRequest) (discord.SentMessage, error)
	React(context.Context, discord.ReactRequest) error
	EditMessage(context.Context, discord.EditRequest) (discord.SentMessage, error)
	SendTyping(context.Context, string) error
	FetchHistory(context.Context, discord.HistoryRequest) ([]discord.Message, error)
	DownloadAttachments(context.Context, discord.DownloadRequest) ([]discord.DownloadedFile, error)
	SetStatus(context.Context, discord.StatusRequest) error
}

// --- Types ---

// DiscordGoClient adapts discordgo to the internal client interface.
type DiscordGoClient struct {
	session    *discordgo.Session
	httpClient *http.Client
	mu         sync.RWMutex
	botUserID  string
}

// --- Constructors ---

// NewDiscordGoClient creates a discordgo-backed client.
func NewDiscordGoClient(token string, intents discordgo.Intent) (*DiscordGoClient, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create discord session: %w", err)
	}

	session.StateEnabled = true
	session.Identify.Intents = intents

	client := &DiscordGoClient{
		session: session,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	session.AddHandler(func(_ *discordgo.Session, ready *discordgo.Ready) {
		if ready != nil && ready.User != nil {
			client.mu.Lock()
			client.botUserID = ready.User.ID
			client.mu.Unlock()
		}
	})

	return client, nil
}

// --- Lifecycle ---

// Open opens the Discord gateway session.
func (c *DiscordGoClient) Open() error {
	return c.session.Open()
}

// Close closes the Discord gateway session.
func (c *DiscordGoClient) Close() error {
	return c.session.Close()
}

// BotUserID returns the current bot user id.
func (c *DiscordGoClient) BotUserID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.botUserID
}

// OnMessageCreate registers a raw message handler.
func (c *DiscordGoClient) OnMessageCreate(handler func(context.Context, discord.RawMessageEvent)) func() {
	return c.session.AddHandler(func(session *discordgo.Session, message *discordgo.MessageCreate) {
		handler(context.Background(), mapRawMessage(session, message))
	})
}

// --- Message Operations ---

// SendMessage sends a Discord message.
func (c *DiscordGoClient) SendMessage(ctx context.Context, req discord.SendRequest) (discord.SentMessage, error) {
	messageSend, cleanup, err := buildMessageSend(req.Text, req.Files, nil)
	if err != nil {
		return discord.SentMessage{}, err
	}
	defer cleanup()

	sent, err := c.session.ChannelMessageSendComplex(req.ChannelID, messageSend, discordgo.WithContext(ctx))
	if err != nil {
		return discord.SentMessage{}, fmt.Errorf("failed to send message: %w", err)
	}

	return discord.SentMessage{
		ChannelID: sent.ChannelID,
		MessageID: sent.ID,
		Content:   sent.Content,
	}, nil
}

// SendTyping fires a typing indicator in the given channel. The indicator
// lasts for roughly 10 seconds in Discord clients or until the bot sends a
// message - call it right before composing a long reply.
func (c *DiscordGoClient) SendTyping(ctx context.Context, channelID string) error {
	if err := c.session.ChannelTyping(channelID, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to send typing indicator: %w", err)
	}
	return nil
}

// Reply sends a Discord reply.
func (c *DiscordGoClient) Reply(ctx context.Context, req discord.ReplyRequest) (discord.SentMessage, error) {
	failIfNotExists := false
	reference := &discordgo.MessageReference{
		MessageID:       req.ReplyToMessageID,
		ChannelID:       req.ChannelID,
		GuildID:         req.GuildID,
		FailIfNotExists: &failIfNotExists,
	}
	messageSend, cleanup, err := buildMessageSend(req.Text, req.Files, reference)
	if err != nil {
		return discord.SentMessage{}, err
	}
	defer cleanup()

	sent, err := c.session.ChannelMessageSendComplex(req.ChannelID, messageSend, discordgo.WithContext(ctx))
	if err != nil {
		return discord.SentMessage{}, fmt.Errorf("failed to send reply: %w", err)
	}

	return discord.SentMessage{
		ChannelID: sent.ChannelID,
		MessageID: sent.ID,
		Content:   sent.Content,
	}, nil
}

// React adds an emoji reaction to a Discord message.
func (c *DiscordGoClient) React(ctx context.Context, req discord.ReactRequest) error {
	if err := c.session.MessageReactionAdd(req.ChannelID, req.MessageID, req.Emoji, discordgo.WithContext(ctx)); err != nil {
		return fmt.Errorf("failed to add reaction: %w", err)
	}
	return nil
}

// EditMessage edits a Discord message.
func (c *DiscordGoClient) EditMessage(ctx context.Context, req discord.EditRequest) (discord.SentMessage, error) {
	edited, err := c.session.ChannelMessageEditComplex(
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

// FetchHistory returns normalized Discord channel history.
func (c *DiscordGoClient) FetchHistory(ctx context.Context, req discord.HistoryRequest) ([]discord.Message, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	messages, err := c.session.ChannelMessages(
		req.ChannelID,
		limit,
		req.BeforeID,
		req.AfterID,
		req.AroundID,
		discordgo.WithContext(ctx),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch history: %w", err)
	}

	result := make([]discord.Message, 0, len(messages))
	for idx := len(messages) - 1; idx >= 0; idx-- {
		result = append(result, normalizeMessage(c.BotUserID(), messages[idx]))
	}

	return result, nil
}

// DownloadAttachments downloads Discord attachments to disk.
func (c *DiscordGoClient) DownloadAttachments(ctx context.Context, req discord.DownloadRequest) ([]discord.DownloadedFile, error) {
	if req.DestinationDir == "" {
		return nil, fmt.Errorf("download destination dir must not be empty")
	}
	if req.MaxBytes < 1 {
		return nil, fmt.Errorf("download max bytes must be at least 1")
	}

	message, err := c.session.ChannelMessage(req.ChannelID, req.MessageID, discordgo.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch message for attachments: %w", err)
	}
	if err := os.MkdirAll(req.DestinationDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create destination dir %s: %w", req.DestinationDir, err)
	}

	results := make([]discord.DownloadedFile, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		if req.AttachmentID != "" && attachment.ID != req.AttachmentID {
			continue
		}
		if int64(attachment.Size) > req.MaxBytes {
			return nil, fmt.Errorf("attachment %s exceeds max bytes", attachment.ID)
		}

		downloaded, err := c.downloadAttachment(ctx, attachment, req.DestinationDir, req.MaxBytes)
		if err != nil {
			return nil, err
		}
		results = append(results, downloaded)
	}

	return results, nil
}

// SetStatus updates the Discord presence and activity.
func (c *DiscordGoClient) SetStatus(ctx context.Context, req discord.StatusRequest) error {
	activities := make([]*discordgo.Activity, 0, 1)
	if req.ActivityText != "" {
		activityType, err := mapActivityType(req.ActivityType)
		if err != nil {
			return err
		}
		activities = append(activities, &discordgo.Activity{
			Name: req.ActivityText,
			Type: activityType,
		})
	}

	statusData := discordgo.UpdateStatusData{
		Activities: activities,
		Status:     req.Presence,
	}
	if err := c.session.UpdateStatusComplex(statusData); err != nil {
		return fmt.Errorf("failed to set status: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

// --- Attachment Helpers ---

func (c *DiscordGoClient) downloadAttachment(
	ctx context.Context,
	attachment *discordgo.MessageAttachment,
	destinationDir string,
	maxBytes int64,
) (discord.DownloadedFile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, attachment.URL, nil)
	if err != nil {
		return discord.DownloadedFile{}, fmt.Errorf("failed to create attachment request: %w", err)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return discord.DownloadedFile{}, fmt.Errorf("failed to download attachment %s: %w", attachment.ID, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return discord.DownloadedFile{}, fmt.Errorf("attachment download returned status %d", response.StatusCode)
	}

	limitedReader := io.LimitReader(response.Body, maxBytes+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return discord.DownloadedFile{}, fmt.Errorf("failed to read attachment %s: %w", attachment.ID, err)
	}
	if int64(len(body)) > maxBytes {
		return discord.DownloadedFile{}, fmt.Errorf("attachment %s exceeds max bytes", attachment.ID)
	}

	filename := buildDownloadedFilename(attachment.ID, attachment.Filename)
	path := filepath.Join(destinationDir, filename)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		return discord.DownloadedFile{}, fmt.Errorf("failed to write attachment %s: %w", attachment.ID, err)
	}

	return discord.DownloadedFile{
		AttachmentID: attachment.ID,
		Filename:     filename,
		Path:         path,
		ContentType:  attachment.ContentType,
		SizeBytes:    int64(len(body)),
	}, nil
}

// --- Message Helpers ---

func buildMessageSend(
	text string,
	paths []string,
	reference *discordgo.MessageReference,
) (*discordgo.MessageSend, func(), error) {
	files, cleanup, err := openFiles(paths)
	if err != nil {
		return nil, nil, err
	}

	messageSend := &discordgo.MessageSend{
		Content: text,
		Files:   files,
		AllowedMentions: &discordgo.MessageAllowedMentions{
			RepliedUser: false,
		},
		Reference: reference,
	}

	return messageSend, cleanup, nil
}

func openFiles(paths []string) ([]*discordgo.File, func(), error) {
	if len(paths) == 0 {
		return nil, func() {}, nil
	}

	files := make([]*discordgo.File, 0, len(paths))
	closers := make([]io.Closer, 0, len(paths))
	cleanup := func() {
		for _, closer := range closers {
			_ = closer.Close()
		}
	}

	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to open file %s: %w", path, err)
		}
		closers = append(closers, file)
		files = append(files, &discordgo.File{
			Name:   filepath.Base(path),
			Reader: file,
		})
	}

	return files, cleanup, nil
}

// --- Filename Helpers ---

func buildDownloadedFilename(attachmentID string, original string) string {
	base := filepath.Base(original)
	base = strings.ReplaceAll(base, "/", "_")
	base = strings.ReplaceAll(base, "\\", "_")
	base = strings.ReplaceAll(base, " ", "_")
	if base == "." || base == "" {
		base = "attachment"
	}

	ext := filepath.Ext(base)
	if ext == "" {
		return attachmentID
	}
	return attachmentID + ext
}

// --- Normalization Helpers ---

func mapRawMessage(session *discordgo.Session, message *discordgo.MessageCreate) discord.RawMessageEvent {
	mentionedUserIDs := make([]string, 0, len(message.Mentions))
	for _, mention := range message.Mentions {
		mentionedUserIDs = append(mentionedUserIDs, mention.ID)
	}

	roleIDs := make([]string, 0)
	if message.Member != nil {
		roleIDs = append(roleIDs, message.Member.Roles...)
	}

	attachments := make([]discord.Attachment, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, discord.Attachment{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			SizeBytes:   int64(attachment.Size),
			URL:         attachment.URL,
		})
	}

	var referencedMessageID string
	var referencedAuthorID string
	if message.MessageReference != nil {
		referencedMessageID = message.MessageReference.MessageID
	}
	if message.ReferencedMessage != nil && message.ReferencedMessage.Author != nil {
		referencedAuthorID = message.ReferencedMessage.Author.ID
	}

	channelKind, threadParentID := resolveChannelMetadata(session, message)

	return discord.RawMessageEvent{
		ID:                  message.ID,
		ChannelID:           message.ChannelID,
		GuildID:             message.GuildID,
		ThreadParentID:      threadParentID,
		ChannelKind:         channelKind,
		AuthorID:            message.Author.ID,
		AuthorUsername:      message.Author.Username,
		RoleIDs:             roleIDs,
		AuthorBot:           message.Author.Bot,
		WebhookID:           message.WebhookID,
		Content:             message.Content,
		MentionedUserIDs:    mentionedUserIDs,
		ReferencedMessageID: referencedMessageID,
		ReferencedAuthorID:  referencedAuthorID,
		Attachments:         attachments,
		Timestamp:           message.Timestamp,
	}
}

func resolveChannelMetadata(session *discordgo.Session, message *discordgo.MessageCreate) (discord.ChannelKind, string) {
	if session != nil && session.State != nil {
		channel, err := session.State.Channel(message.ChannelID)
		if err == nil && channel != nil {
			kind := mapChannelType(channel.Type, channel.GuildID)
			// channel.ParentID is the category for regular text channels and
			// the parent channel for threads. Only surface it as
			// ThreadParentID for thread channels so access evaluation does
			// not accidentally substitute a category ID for the channel ID.
			var threadParentID string
			if kind == discord.ChannelKindPublicThread || kind == discord.ChannelKindPrivateThread {
				threadParentID = channel.ParentID
			}
			return kind, threadParentID
		}
	}
	return mapChannelType(0, message.GuildID), ""
}

func mapChannelType(channelType discordgo.ChannelType, guildID string) discord.ChannelKind {
	switch {
	case guildID == "":
		return discord.ChannelKindDM
	case channelType == discordgo.ChannelTypeGuildPublicThread:
		return discord.ChannelKindPublicThread
	case channelType == discordgo.ChannelTypeGuildPrivateThread:
		return discord.ChannelKindPrivateThread
	default:
		return discord.ChannelKindGuildText
	}
}

func normalizeMessage(botUserID string, message *discordgo.Message) discord.Message {
	mentionedBot := false
	for _, mention := range message.Mentions {
		if mention.ID == botUserID {
			mentionedBot = true
			break
		}
	}

	attachments := make([]discord.Attachment, 0, len(message.Attachments))
	for _, attachment := range message.Attachments {
		attachments = append(attachments, discord.Attachment{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			SizeBytes:   int64(attachment.Size),
			URL:         attachment.URL,
		})
	}

	repliedToBot := false
	if message.ReferencedMessage != nil && message.ReferencedMessage.Author != nil {
		repliedToBot = message.ReferencedMessage.Author.ID == botUserID
	}

	roleIDs := make([]string, 0)
	if message.Member != nil {
		roleIDs = append(roleIDs, message.Member.Roles...)
	}

	channelKind := discord.ChannelKindGuildText
	if message.GuildID == "" {
		channelKind = discord.ChannelKindDM
	}

	return discord.Message{
		ID:             message.ID,
		ChannelID:      message.ChannelID,
		GuildID:        message.GuildID,
		ChannelKind:    channelKind,
		AuthorID:       message.Author.ID,
		AuthorUsername: message.Author.Username,
		RoleIDs:        roleIDs,
		Content:        message.Content,
		MentionedBot:   mentionedBot,
		RepliedToBot:   repliedToBot,
		Attachments:    attachments,
		Timestamp:      message.Timestamp,
	}
}

// --- Activity Helpers ---

func mapActivityType(value string) (discordgo.ActivityType, error) {
	switch value {
	case "", "playing":
		return discordgo.ActivityTypeGame, nil
	case "streaming":
		return discordgo.ActivityTypeStreaming, nil
	case "listening":
		return discordgo.ActivityTypeListening, nil
	case "watching":
		return discordgo.ActivityTypeWatching, nil
	case "competing":
		return discordgo.ActivityTypeCompeting, nil
	default:
		return 0, fmt.Errorf("unsupported activity type %q", value)
	}
}

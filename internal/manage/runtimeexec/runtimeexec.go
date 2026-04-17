package runtimeexec

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
)

// --- Types ---

// ManagerClient exposes the Discord manager surface to interpreted scripts.
type ManagerClient struct {
	ctx     context.Context
	manager discord.Manager
}

// --- Constructors ---

// NewManagerClient creates a script-facing manager client.
func NewManagerClient(ctx context.Context, manager discord.Manager) *ManagerClient {
	return &ManagerClient{
		ctx:     ctx,
		manager: manager,
	}
}

// --- Guild Methods ---

// GetGuild returns guild metadata.
func (c *ManagerClient) GetGuild(guildID string) (discord.GuildInfo, error) {
	return c.manager.GetGuild(c.ctx, guildID)
}

// ListChannels returns guild channels.
func (c *ManagerClient) ListChannels(guildID string) ([]discord.GuildChannel, error) {
	return c.manager.ListChannels(c.ctx, guildID)
}

// ListRoles returns guild roles.
func (c *ManagerClient) ListRoles(guildID string) ([]discord.GuildRole, error) {
	return c.manager.ListRoles(c.ctx, guildID)
}

// ListMembers returns guild members.
func (c *ManagerClient) ListMembers(guildID string, limit int) ([]discord.GuildMember, error) {
	return c.manager.ListMembers(c.ctx, discord.ListMembersRequest{
		GuildID: guildID,
		Limit:   limit,
	})
}

// GetMember returns guild member metadata.
func (c *ManagerClient) GetMember(guildID string, userID string) (discord.GuildMember, error) {
	return c.manager.GetMember(c.ctx, discord.GetMemberRequest{
		GuildID: guildID,
		UserID:  userID,
	})
}

// --- Channel Methods ---

// CreateChannel creates a channel.
func (c *ManagerClient) CreateChannel(guildID string, name string, channelType string, parentID string, topic string) (discord.GuildChannel, error) {
	return c.manager.CreateChannel(c.ctx, discord.ChannelCreateRequest{
		GuildID:  guildID,
		Name:     name,
		Type:     channelType,
		ParentID: parentID,
		Topic:    topic,
	})
}

// UpdateChannel updates a channel.
func (c *ManagerClient) UpdateChannel(channelID string, name *string, topic *string, parentID *string) (discord.GuildChannel, error) {
	return c.manager.UpdateChannel(c.ctx, discord.ChannelUpdateRequest{
		ID:       channelID,
		Name:     name,
		Topic:    topic,
		ParentID: parentID,
	})
}

// DeleteChannel deletes a channel.
func (c *ManagerClient) DeleteChannel(channelID string) error {
	return c.manager.DeleteChannel(c.ctx, channelID)
}

// SetChannelPermission sets a channel permission overwrite.
func (c *ManagerClient) SetChannelPermission(channelID string, targetID string, targetType string, allow []string, deny []string) error {
	return c.manager.SetChannelPermission(c.ctx, discord.ChannelPermissionSetRequest{
		ChannelID:  channelID,
		TargetID:   targetID,
		TargetType: targetType,
		Allow:      allow,
		Deny:       deny,
	})
}

// RemoveChannelPermission removes a channel permission overwrite.
func (c *ManagerClient) RemoveChannelPermission(channelID string, targetID string, targetType string) error {
	return c.manager.RemoveChannelPermission(c.ctx, discord.ChannelPermissionRemoveRequest{
		ChannelID:  channelID,
		TargetID:   targetID,
		TargetType: targetType,
	})
}

// --- Role Methods ---

// CreateRole creates a guild role.
func (c *ManagerClient) CreateRole(guildID string, name string, color int, hoist bool, mentionable bool) (discord.GuildRole, error) {
	return c.manager.CreateRole(c.ctx, discord.RoleCreateRequest{
		GuildID:     guildID,
		Name:        name,
		Color:       intPointer(color),
		Hoist:       boolPointer(hoist),
		Mentionable: boolPointer(mentionable),
	})
}

// UpdateRole updates a guild role.
func (c *ManagerClient) UpdateRole(guildID string, roleID string, name string, color int, hoist bool, mentionable bool) (discord.GuildRole, error) {
	return c.manager.UpdateRole(c.ctx, discord.RoleUpdateRequest{
		GuildID:     guildID,
		RoleID:      roleID,
		Name:        stringPointer(name),
		Color:       intPointer(color),
		Hoist:       boolPointer(hoist),
		Mentionable: boolPointer(mentionable),
	})
}

// DeleteRole deletes a guild role.
func (c *ManagerClient) DeleteRole(guildID string, roleID string) error {
	return c.manager.DeleteRole(c.ctx, discord.RoleDeleteRequest{
		GuildID: guildID,
		RoleID:  roleID,
	})
}

// AssignRole assigns a role.
func (c *ManagerClient) AssignRole(guildID string, userID string, roleID string) error {
	return c.manager.AssignRole(c.ctx, discord.RoleAssignmentRequest{
		GuildID: guildID,
		UserID:  userID,
		RoleID:  roleID,
	})
}

// UnassignRole removes a role.
func (c *ManagerClient) UnassignRole(guildID string, userID string, roleID string) error {
	return c.manager.UnassignRole(c.ctx, discord.RoleAssignmentRequest{
		GuildID: guildID,
		UserID:  userID,
		RoleID:  roleID,
	})
}

// --- Member Methods ---

// KickMember removes a member from a guild.
func (c *ManagerClient) KickMember(guildID string, userID string) error {
	return c.manager.KickMember(c.ctx, discord.GuildUserRequest{
		GuildID: guildID,
		UserID:  userID,
	})
}

// BanMember bans a member from a guild.
func (c *ManagerClient) BanMember(guildID string, userID string) error {
	return c.manager.BanMember(c.ctx, discord.GuildUserRequest{
		GuildID: guildID,
		UserID:  userID,
	})
}

// --- Message Methods ---

// SendMessage sends a Discord message.
func (c *ManagerClient) SendMessage(channelID string, text string, files []string) (discord.SentMessage, error) {
	return c.manager.SendManagedMessage(c.ctx, discord.SendRequest{
		ChannelID: channelID,
		Text:      text,
		Files:     files,
	})
}

// EditMessage edits a Discord message.
func (c *ManagerClient) EditMessage(channelID string, messageID string, text string) (discord.SentMessage, error) {
	return c.manager.EditManagedMessage(c.ctx, discord.EditRequest{
		ChannelID: channelID,
		MessageID: messageID,
		Text:      text,
	})
}

// DeleteMessage deletes a Discord message.
func (c *ManagerClient) DeleteMessage(channelID string, messageID string) error {
	return c.manager.DeleteManagedMessage(c.ctx, discord.MessageTarget{
		ChannelID: channelID,
		MessageID: messageID,
	})
}

// BulkDeleteMessages bulk deletes Discord messages for a user.
func (c *ManagerClient) BulkDeleteMessages(channelID string, userID string, beforeID string) (discord.BulkDeleteResult, error) {
	return c.manager.BulkDeleteMessages(c.ctx, discord.BulkDeleteRequest{
		ChannelID: channelID,
		UserID:    userID,
		BeforeID:  beforeID,
	})
}

// ReactToMessage adds a reaction.
func (c *ManagerClient) ReactToMessage(channelID string, messageID string, emoji string) error {
	return c.manager.ReactToManagedMessage(c.ctx, discord.ReactRequest{
		ChannelID: channelID,
		MessageID: messageID,
		Emoji:     emoji,
	})
}

// --- Higher-Level Methods ---

// PostEmbed posts an embed message.
func (c *ManagerClient) PostEmbed(channelID string, title string, description string, color int, image string, fields []discord.EmbedField) (discord.SentMessage, error) {
	return c.manager.PostEmbed(c.ctx, discord.EmbedPostRequest{
		ChannelID:   channelID,
		Title:       title,
		Description: description,
		Color:       intPointer(color),
		Image:       image,
		Fields:      fields,
	})
}

// AddButton appends a button to a message.
func (c *ManagerClient) AddButton(channelID string, messageID string, label string, customID string, style string) (discord.SentMessage, error) {
	return c.manager.AddButton(c.ctx, discord.ButtonAddRequest{
		ChannelID: channelID,
		MessageID: messageID,
		Label:     label,
		CustomID:  customID,
		Style:     style,
	})
}

// AddSelect appends a select menu to a message.
func (c *ManagerClient) AddSelect(channelID string, messageID string, customID string, options []discord.SelectOption) (discord.SentMessage, error) {
	return c.manager.AddSelect(c.ctx, discord.SelectAddRequest{
		ChannelID: channelID,
		MessageID: messageID,
		CustomID:  customID,
		Options:   options,
	})
}

// RespondInteraction sends a raw interaction response.
func (c *ManagerClient) RespondInteraction(interactionID string, interactionToken string, responseType int, body []byte) error {
	return c.manager.RespondInteraction(c.ctx, discord.InteractionResponseRequest{
		InteractionID:    interactionID,
		InteractionToken: interactionToken,
		Type:             responseType,
		Body:             body,
	})
}

// REST executes a raw Discord REST request.
func (c *ManagerClient) REST(method string, path string, body []byte) (discord.RESTResponse, error) {
	return c.manager.REST(c.ctx, discord.RESTRequest{
		Method: method,
		Path:   path,
		Body:   body,
	})
}

// --- Runtime ---

// ExecuteSource executes a yaegi script against the manager surface.
func ExecuteSource(ctx context.Context, manager discord.Manager, source string) error {
	packageName, err := detectPackageName(source)
	if err != nil {
		return err
	}
	if err := validateImports(source); err != nil {
		return err
	}

	timeout, err := loadExecTimeout()
	if err != nil {
		return err
	}

	interpreter := interp.New(interp.Options{})
	if err := interpreter.Use(allowedSymbols()); err != nil {
		return fmt.Errorf("load yaegi sandbox symbols: %w", err)
	}
	if err := interpreter.Use(exports()); err != nil {
		return fmt.Errorf("load manager exports: %w", err)
	}

	prelude := fmt.Sprintf("package %s\nimport runtimeexecpkg %q\ntype ManagerClient = runtimeexecpkg.ManagerClient\n", packageName, packageImportPath)
	if _, err := interpreter.Eval(prelude); err != nil {
		return fmt.Errorf("eval yaegi prelude: %w", err)
	}
	if _, err := interpreter.Eval(source); err != nil {
		return fmt.Errorf("eval yaegi source: %w", err)
	}

	runValue, err := interpreter.Eval(packageName + ".Run")
	if err != nil {
		return fmt.Errorf("resolve Run function: %w", err)
	}
	if err := validateRunSignature(runValue); err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := NewManagerClient(runCtx, manager)
	resultCh := make(chan error, 1)
	go func() {
		callErr := func() (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("script panicked: %v", r)
				}
			}()

			results := runValue.Call([]reflect.Value{reflect.ValueOf(client)})
			if len(results) != 1 {
				return fmt.Errorf("Run must return exactly one value")
			}
			if errValue := results[0].Interface(); errValue != nil {
				return errValue.(error)
			}

			return nil
		}()

		select {
		case resultCh <- callErr:
		case <-runCtx.Done():
		}
	}()

	select {
	case err := <-resultCh:
		return err
	case <-runCtx.Done():
		return fmt.Errorf("run script: %w", runCtx.Err())
	}
}

// --- Internal Helpers ---

const packageImportPath = "github.com/alxxpersonal/exo-discord/internal/manage/runtimeexec"
const defaultExecTimeout = 30 * time.Second
const execTimeoutEnv = "EXO_DISCORD_EXEC_TIMEOUT"

var allowedImports = map[string]struct{}{
	"encoding/json": {},
	"errors":        {},
	"fmt":           {},
	"slices":        {},
	"sort":          {},
	"strings":       {},
	"time":          {},
}

func exports() interp.Exports {
	return interp.Exports{
		packageImportPath + "/runtimeexec": {
			"ManagerClient": reflect.ValueOf((*ManagerClient)(nil)),
		},
	}
}

func allowedSymbols() interp.Exports {
	symbols := interp.Exports{
		"encoding/json/json": stdlib.Symbols["encoding/json/json"],
		"errors/errors":      stdlib.Symbols["errors/errors"],
		"fmt/fmt":            stdlib.Symbols["fmt/fmt"],
		"slices/slices":      stdlib.Symbols["slices/slices"],
		"sort/sort":          stdlib.Symbols["sort/sort"],
		"strings/strings":    stdlib.Symbols["strings/strings"],
		"time/time": {
			"Duration": stdlib.Symbols["time/time"]["Duration"],
			"Now":      stdlib.Symbols["time/time"]["Now"],
		},
	}
	return symbols
}

func detectPackageName(source string) (string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "script.go", source, parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("parse script package: %w", err)
	}
	if file == nil || file.Name == nil {
		return "", fmt.Errorf("script package name not found")
	}

	packageName := strings.TrimSpace(file.Name.Name)
	if packageName != "main" {
		return "", fmt.Errorf("script package must be main, got %q", packageName)
	}
	return packageName, nil
}

func validateImports(source string) error {
	file, err := parser.ParseFile(token.NewFileSet(), "script.go", source, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("parse script imports: %w", err)
	}

	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return fmt.Errorf("decode script import %q: %w", spec.Path.Value, err)
		}
		if _, ok := allowedImports[importPath]; ok {
			continue
		}
		return fmt.Errorf("script import %q is not allowed", importPath)
	}

	return nil
}

func loadExecTimeout() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(execTimeoutEnv))
	if value == "" {
		return defaultExecTimeout, nil
	}

	timeout, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", execTimeoutEnv, err)
	}
	if timeout <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", execTimeoutEnv)
	}
	return timeout, nil
}

func validateRunSignature(runValue reflect.Value) error {
	runType := runValue.Type()
	if runType.Kind() != reflect.Func {
		return fmt.Errorf("Run must be a function")
	}
	if runType.NumIn() != 1 || runType.NumOut() != 1 {
		return fmt.Errorf("Run must have signature func Run(client *ManagerClient) error")
	}
	if runType.In(0) != reflect.TypeOf((*ManagerClient)(nil)) {
		return fmt.Errorf("Run must accept *ManagerClient")
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !runType.Out(0).Implements(errorType) {
		return fmt.Errorf("Run must return error")
	}
	return nil
}

func intPointer(value int) *int {
	return &value
}

func boolPointer(value bool) *bool {
	return &value
}

func stringPointer(value string) *string {
	return &value
}

package scaffold

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/alxxpersonal/exo-discord/internal/manage/colors"
)

// --- Apply Types ---

// ApplyOptions stores scaffold apply options.
type ApplyOptions struct {
	DeleteExtras bool `json:"delete_extras,omitempty"`
}

// ApplyError stores scaffold apply failure metadata.
type ApplyError struct {
	Cause         error
	Applied       Plan
	Rollback      Plan
	RollbackErr   error
	ManualCleanup bool
}

func (e *ApplyError) Error() string {
	if e == nil {
		return ""
	}

	message := fmt.Sprintf("apply scaffold: %v", e.Cause)
	if len(e.Applied.Changes) > 0 {
		message += fmt.Sprintf("; successful changes: %s", summarizeChanges(e.Applied.Changes))
	}
	if e.RollbackErr == nil {
		if len(e.Rollback.Changes) > 0 {
			message += fmt.Sprintf("; rollback changes: %s", summarizeChanges(e.Rollback.Changes))
		}
		return message + "; rollback succeeded"
	}

	if len(e.Rollback.Changes) > 0 {
		message += fmt.Sprintf("; rollback changes: %s", summarizeChanges(e.Rollback.Changes))
	}
	message += fmt.Sprintf("; rollback failed: %v", e.RollbackErr)
	if e.ManualCleanup {
		message += "; guild in mixed state, manual cleanup needed"
	}

	return message
}

// Unwrap returns the underlying apply error.
func (e *ApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type applyContext struct {
	ctx     context.Context
	manager discord.Manager
	spec    Spec
	state   *State
	plan    Plan
}

// --- Plan Helpers ---

// BuildPlan loads live state and calculates a scaffold plan.
func BuildPlan(ctx context.Context, manager discord.Manager, spec Spec) (Plan, error) {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return Plan{}, err
	}

	return Diff(spec, state)
}

// PreparePlan filters plan changes according to apply options.
func PreparePlan(plan Plan, options ApplyOptions) Plan {
	if options.DeleteExtras {
		return plan
	}

	filtered := Plan{
		GuildID:  plan.GuildID,
		Changes:  make([]Change, 0, len(plan.Changes)),
		Warnings: append([]string(nil), plan.Warnings...),
	}
	for _, change := range plan.Changes {
		if change.Action == "delete" {
			filtered.Warnings = append(filtered.Warnings, deleteWarning(change))
			continue
		}
		filtered.Changes = append(filtered.Changes, change)
	}
	return filtered
}

// Apply applies a scaffold spec to Discord and returns the resulting plan.
func Apply(ctx context.Context, manager discord.Manager, spec Spec, options ApplyOptions) (Plan, error) {
	initialState, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return Plan{}, err
	}

	currentState := cloneState(initialState)
	applier := newApplyContext(ctx, manager, spec, &currentState)
	plan, err := applier.apply(options)
	if err == nil {
		return plan, nil
	}

	rollbackState := currentState
	rollbackSpec := specFromState(initialState)
	rollback := newApplyContext(ctx, manager, rollbackSpec, &rollbackState)
	rollbackPlan, rollbackErr := rollback.apply(ApplyOptions{DeleteExtras: true})
	if rollbackErr != nil {
		return plan, &ApplyError{
			Cause:         err,
			Applied:       plan,
			Rollback:      rollbackPlan,
			RollbackErr:   rollbackErr,
			ManualCleanup: true,
		}
	}

	return plan, &ApplyError{
		Cause:    err,
		Applied:  plan,
		Rollback: rollbackPlan,
	}
}

func newApplyContext(ctx context.Context, manager discord.Manager, spec Spec, state *State) *applyContext {
	return &applyContext{
		ctx:     ctx,
		manager: manager,
		spec:    spec,
		state:   state,
		plan: Plan{
			GuildID: spec.Guild.ID,
		},
	}
}

func (a *applyContext) apply(options ApplyOptions) (Plan, error) {
	if !options.DeleteExtras {
		warnings, err := skippedDeleteWarnings(a.spec, *a.state)
		if err != nil {
			return a.plan, err
		}
		a.plan.Warnings = warnings
	}

	if err := a.applyRoles(); err != nil {
		return a.plan, err
	}
	if err := a.applyCategories(); err != nil {
		return a.plan, err
	}
	if err := a.applyChannels(); err != nil {
		return a.plan, err
	}
	if options.DeleteExtras {
		if err := a.deleteExtraChannels(); err != nil {
			return a.plan, err
		}
		if err := a.deleteExtraRoles(); err != nil {
			return a.plan, err
		}
	}

	return a.plan, nil
}

func (a *applyContext) applyRoles() error {
	roleIndex := make(map[string]discord.GuildRole, len(a.state.Roles))
	for _, role := range a.state.Roles {
		roleIndex[role.Name] = role
	}

	for _, role := range a.spec.Roles {
		color, err := colors.ParseOptionalHexColor(role.Color)
		if err != nil {
			return fmt.Errorf("parse role color for %q: %w", role.Name, err)
		}
		current, ok := roleIndex[role.Name]
		if !ok {
			req := discord.RoleCreateRequest{
				GuildID:     a.spec.Guild.ID,
				Name:        role.Name,
				Hoist:       boolPointer(role.Hoist),
				Mentionable: boolPointer(role.Mentionable),
			}
			if color != nil {
				req.Color = color
			}

			created, err := a.manager.CreateRole(a.ctx, req)
			if err != nil {
				return fmt.Errorf("create role %q: %w", role.Name, err)
			}

			a.upsertRole(created)
			roleIndex[role.Name] = created
			a.recordChange(Change{
				Action: "create",
				Kind:   "role",
				Name:   role.Name,
			})
			continue
		}

		req := discord.RoleUpdateRequest{
			GuildID: a.spec.Guild.ID,
			RoleID:  current.ID,
		}
		fields := make([]string, 0, 3)
		if color != nil && current.Color != *color {
			req.Color = color
			fields = append(fields, "color")
		}
		if current.Hoist != role.Hoist {
			req.Hoist = boolPointer(role.Hoist)
			fields = append(fields, "hoist")
		}
		if current.Mentionable != role.Mentionable {
			req.Mentionable = boolPointer(role.Mentionable)
			fields = append(fields, "mentionable")
		}
		if len(fields) == 0 {
			continue
		}

		updated, err := a.manager.UpdateRole(a.ctx, req)
		if err != nil {
			return fmt.Errorf("update role %q: %w", role.Name, err)
		}

		a.upsertRole(mergeRole(current, updated, req))
		a.recordChange(Change{
			Action: "update",
			Kind:   "role",
			Name:   role.Name,
			Fields: fields,
		})
	}

	return nil
}

func (a *applyContext) applyCategories() error {
	return a.applyChannelGroup(a.spec.Categories)
}

func (a *applyContext) applyChannels() error {
	return a.applyChannelGroup(a.spec.Channels)
}

func (a *applyContext) applyChannelGroup(channels []ChannelSpec) error {
	for _, channel := range channels {
		channelIndex := make(map[string]discord.GuildChannel, len(a.state.Channels))
		parentIDs := make(map[string]string, len(a.state.Channels))
		for _, currentChannel := range a.state.Channels {
			channelIndex[channelKey(currentChannel.Type, currentChannel.Name)] = currentChannel
			if currentChannel.Type == "category" {
				parentIDs[currentChannel.Name] = currentChannel.ID
			}
		}

		roleIndex := make(map[string]discord.GuildRole, len(a.state.Roles))
		for _, role := range a.state.Roles {
			roleIndex[role.Name] = role
		}

		parentID := ""
		if strings.TrimSpace(channel.Parent) != "" {
			resolvedParentID, ok := parentIDs[channel.Parent]
			if !ok {
				return fmt.Errorf("scaffold parent category %q not found", channel.Parent)
			}
			parentID = resolvedParentID
		}

		key := channelKey(channel.Type, channel.Name)
		current, ok := channelIndex[key]
		if !ok {
			created, err := a.manager.CreateChannel(a.ctx, discord.ChannelCreateRequest{
				GuildID:  a.spec.Guild.ID,
				Name:     channel.Name,
				Type:     channel.Type,
				ParentID: parentID,
				Topic:    channel.Topic,
			})
			if err != nil {
				return fmt.Errorf("create channel %q: %w", channel.Name, err)
			}

			a.upsertChannel(created)
			if _, err := a.applyOverwrites(created.ID, channel.Overwrites, roleIndex); err != nil {
				return err
			}
			a.recordChange(Change{
				Action: "create",
				Kind:   "channel",
				Name:   channel.Name,
				Parent: channel.Parent,
				Fields: channelFields(channel),
			})
			continue
		}

		req := discord.ChannelUpdateRequest{ID: current.ID}
		fields := make([]string, 0, 3)
		if channel.Type != "category" && strings.TrimSpace(channel.Topic) != strings.TrimSpace(current.Topic) {
			req.Topic = stringPointer(channel.Topic)
			fields = append(fields, "topic")
		}
		if strings.TrimSpace(parentID) != strings.TrimSpace(current.ParentID) {
			req.ParentID = stringPointer(parentID)
			fields = append(fields, "parent")
		}
		if len(fields) > 0 {
			updated, err := a.manager.UpdateChannel(a.ctx, req)
			if err != nil {
				return fmt.Errorf("update channel %q: %w", channel.Name, err)
			}
			current = mergeChannel(current, updated, req)
			a.upsertChannel(current)
		}

		overwriteChanged, err := a.applyOverwrites(current.ID, channel.Overwrites, roleIndex)
		if err != nil {
			return err
		}
		if overwriteChanged && !slices.Contains(fields, "overwrites") {
			fields = append(fields, "overwrites")
		}
		if len(fields) == 0 {
			continue
		}

		a.recordChange(Change{
			Action: "update",
			Kind:   "channel",
			Name:   channel.Name,
			Parent: channel.Parent,
			Fields: fields,
		})
	}

	return nil
}

func (a *applyContext) applyOverwrites(
	channelID string,
	desired []OverwriteSpec,
	roleIndex map[string]discord.GuildRole,
) (bool, error) {
	channel, ok := a.channelByID(channelID)
	if !ok {
		return false, fmt.Errorf("channel %q not found in scaffold state", channelID)
	}

	changed := false
	desiredByRole := make(map[string]OverwriteSpec, len(desired))
	for _, overwrite := range desired {
		desiredByRole[overwrite.Role] = overwrite
	}

	idToRoleName := make(map[string]string, len(roleIndex))
	for name, role := range roleIndex {
		idToRoleName[role.ID] = name
	}

	remaining := make([]discord.PermissionOverwrite, 0, len(channel.PermissionOverwrites))
	for _, overwrite := range channel.PermissionOverwrites {
		if overwrite.TargetType != "role" {
			remaining = append(remaining, overwrite)
			continue
		}

		name, exists := idToRoleName[overwrite.TargetID]
		if !exists {
			remaining = append(remaining, overwrite)
			continue
		}
		if _, ok := desiredByRole[name]; ok {
			remaining = append(remaining, overwrite)
			continue
		}

		if err := a.manager.RemoveChannelPermission(a.ctx, discord.ChannelPermissionRemoveRequest{
			ChannelID:  channel.ID,
			TargetID:   overwrite.TargetID,
			TargetType: "role",
		}); err != nil {
			return changed, fmt.Errorf("remove overwrite for role %q on channel %q: %w", name, channel.Name, err)
		}
		changed = true
	}
	channel.PermissionOverwrites = remaining
	a.upsertChannel(channel)

	for _, overwrite := range desired {
		role, ok := roleIndex[overwrite.Role]
		if !ok {
			return changed, fmt.Errorf("scaffold overwrite role %q not found", overwrite.Role)
		}

		currentOverwrite, exists := findRoleOverwrite(channel.PermissionOverwrites, role.ID)
		if exists && overwriteMatchesSpec(currentOverwrite, overwrite) {
			continue
		}

		if err := a.manager.SetChannelPermission(a.ctx, discord.ChannelPermissionSetRequest{
			ChannelID:  channel.ID,
			TargetID:   role.ID,
			TargetType: "role",
			Allow:      overwrite.Allow,
			Deny:       overwrite.Deny,
		}); err != nil {
			return changed, fmt.Errorf("set overwrite for role %q on channel %q: %w", overwrite.Role, channel.Name, err)
		}

		channel.PermissionOverwrites = upsertOverwrite(channel.PermissionOverwrites, discord.PermissionOverwrite{
			TargetID:   role.ID,
			TargetType: "role",
			AllowNames: append([]string(nil), overwrite.Allow...),
			DenyNames:  append([]string(nil), overwrite.Deny...),
		})
		changed = true
	}

	a.upsertChannel(channel)
	return changed, nil
}

func (a *applyContext) deleteExtraChannels() error {
	desired := make(map[string]struct{}, len(a.spec.Categories)+len(a.spec.Channels))
	for _, channel := range desiredChannelSpecs(a.spec) {
		desired[channelKey(channel.Type, channel.Name)] = struct{}{}
	}

	deleteGroup := func(channelType string) error {
		channels := append([]discord.GuildChannel(nil), a.state.Channels...)
		for _, channel := range channels {
			if channel.Type != channelType {
				continue
			}
			if _, ok := desired[channelKey(channel.Type, channel.Name)]; ok {
				continue
			}

			if err := a.manager.DeleteChannel(a.ctx, channel.ID); err != nil {
				return fmt.Errorf("delete channel %q: %w", channel.Name, err)
			}

			a.removeChannel(channel.ID)
			a.recordChange(Change{
				Action: "delete",
				Kind:   "channel",
				Name:   channel.Name,
				Parent: parentName(channel.ParentID, channels),
			})
		}
		return nil
	}

	if err := deleteGroup("text"); err != nil {
		return err
	}
	if err := deleteGroup("voice"); err != nil {
		return err
	}
	if err := deleteGroup("category"); err != nil {
		return err
	}

	return nil
}

func (a *applyContext) deleteExtraRoles() error {
	desired := make(map[string]struct{}, len(a.spec.Roles))
	for _, role := range a.spec.Roles {
		desired[role.Name] = struct{}{}
	}

	roles := append([]discord.GuildRole(nil), a.state.Roles...)
	for _, role := range roles {
		if role.Name == "@everyone" {
			continue
		}
		if _, ok := desired[role.Name]; ok {
			continue
		}

		if err := a.manager.DeleteRole(a.ctx, discord.RoleDeleteRequest{
			GuildID: a.spec.Guild.ID,
			RoleID:  role.ID,
		}); err != nil {
			return fmt.Errorf("delete role %q: %w", role.Name, err)
		}

		a.removeRole(role.ID)
		a.recordChange(Change{
			Action: "delete",
			Kind:   "role",
			Name:   role.Name,
		})
	}

	return nil
}

func (a *applyContext) recordChange(change Change) {
	a.plan.Changes = append(a.plan.Changes, change)
}

func (a *applyContext) channelByID(channelID string) (discord.GuildChannel, bool) {
	for _, channel := range a.state.Channels {
		if channel.ID == channelID {
			return channel, true
		}
	}
	return discord.GuildChannel{}, false
}

func (a *applyContext) upsertChannel(channel discord.GuildChannel) {
	for idx := range a.state.Channels {
		if a.state.Channels[idx].ID == channel.ID {
			a.state.Channels[idx] = channel
			return
		}
	}
	a.state.Channels = append(a.state.Channels, channel)
}

func (a *applyContext) removeChannel(channelID string) {
	filtered := a.state.Channels[:0]
	for _, channel := range a.state.Channels {
		if channel.ID == channelID {
			continue
		}
		filtered = append(filtered, channel)
	}
	a.state.Channels = filtered
}

func (a *applyContext) upsertRole(role discord.GuildRole) {
	for idx := range a.state.Roles {
		if a.state.Roles[idx].ID == role.ID {
			a.state.Roles[idx] = role
			return
		}
	}
	a.state.Roles = append(a.state.Roles, role)
}

func (a *applyContext) removeRole(roleID string) {
	filtered := a.state.Roles[:0]
	for _, role := range a.state.Roles {
		if role.ID == roleID {
			continue
		}
		filtered = append(filtered, role)
	}
	a.state.Roles = filtered
}

func skippedDeleteWarnings(spec Spec, state State) ([]string, error) {
	plan, err := Diff(spec, state)
	if err != nil {
		return nil, err
	}
	return PreparePlan(plan, ApplyOptions{}).Warnings, nil
}

func summarizeChanges(changes []Change) string {
	summaries := make([]string, 0, len(changes))
	for _, change := range changes {
		summary := change.Action + " " + change.Kind + " " + strconvQuote(change.Name)
		if strings.TrimSpace(change.Parent) != "" {
			summary += " in " + strconvQuote(change.Parent)
		}
		if len(change.Fields) > 0 {
			summary += " (" + strings.Join(change.Fields, ", ") + ")"
		}
		summaries = append(summaries, summary)
	}
	return strings.Join(summaries, ", ")
}

func deleteWarning(change Change) string {
	return fmt.Sprintf("keeping existing %s %q because delete extras is disabled", change.Kind, change.Name)
}

func specFromState(state State) Spec {
	spec := Spec{
		Guild: GuildSpec{ID: state.GuildID},
		Roles: make([]RoleSpec, 0, len(state.Roles)),
	}

	for _, role := range state.Roles {
		if role.Name == "@everyone" {
			continue
		}
		spec.Roles = append(spec.Roles, RoleSpec{
			Name:        role.Name,
			Color:       fmt.Sprintf("#%06x", role.Color),
			Hoist:       role.Hoist,
			Mentionable: role.Mentionable,
		})
	}
	sort.Slice(spec.Roles, func(i, j int) bool {
		return spec.Roles[i].Name < spec.Roles[j].Name
	})

	roleNames := make(map[string]string, len(state.Roles))
	for _, role := range state.Roles {
		roleNames[role.ID] = role.Name
	}

	channelNames := make(map[string]string, len(state.Channels))
	for _, channel := range state.Channels {
		channelNames[channel.ID] = channel.Name
	}

	spec.Categories = make([]ChannelSpec, 0)
	spec.Channels = make([]ChannelSpec, 0)
	for _, channel := range state.Channels {
		channelSpec := ChannelSpec{
			Name:       channel.Name,
			Type:       channel.Type,
			Topic:      channel.Topic,
			Parent:     channelNames[channel.ParentID],
			Overwrites: overwritesFromState(channel.PermissionOverwrites, roleNames),
		}
		if channel.Type == "category" {
			spec.Categories = append(spec.Categories, channelSpec)
			continue
		}
		spec.Channels = append(spec.Channels, channelSpec)
	}

	normalizeSpec(&spec)
	return spec
}

func overwritesFromState(
	overwrites []discord.PermissionOverwrite,
	roleNames map[string]string,
) []OverwriteSpec {
	result := make([]OverwriteSpec, 0, len(overwrites))
	for _, overwrite := range overwrites {
		if overwrite.TargetType != "role" {
			continue
		}
		roleName, ok := roleNames[overwrite.TargetID]
		if !ok {
			continue
		}

		allow := append([]string(nil), overwrite.AllowNames...)
		deny := append([]string(nil), overwrite.DenyNames...)
		sort.Strings(allow)
		sort.Strings(deny)
		result = append(result, OverwriteSpec{
			Role:  roleName,
			Allow: allow,
			Deny:  deny,
		})
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Role < result[j].Role
	})
	return result
}

func cloneState(state State) State {
	cloned := State{
		GuildID:  state.GuildID,
		Roles:    make([]discord.GuildRole, 0, len(state.Roles)),
		Channels: make([]discord.GuildChannel, 0, len(state.Channels)),
	}

	for _, role := range state.Roles {
		cloned.Roles = append(cloned.Roles, role)
	}
	for _, channel := range state.Channels {
		channelCopy := channel
		channelCopy.PermissionOverwrites = append([]discord.PermissionOverwrite(nil), channel.PermissionOverwrites...)
		cloned.Channels = append(cloned.Channels, channelCopy)
	}

	return cloned
}

func mergeRole(current discord.GuildRole, updated discord.GuildRole, req discord.RoleUpdateRequest) discord.GuildRole {
	merged := current
	if updated.ID != "" {
		merged.ID = updated.ID
	}
	if updated.GuildID != "" {
		merged.GuildID = updated.GuildID
	}
	if req.Name != nil {
		merged.Name = *req.Name
	}
	if req.Color != nil {
		merged.Color = *req.Color
		merged.ColorHex = fmt.Sprintf("#%06x", *req.Color)
	}
	if req.Hoist != nil {
		merged.Hoist = *req.Hoist
	}
	if req.Mentionable != nil {
		merged.Mentionable = *req.Mentionable
	}
	if updated.Position != 0 {
		merged.Position = updated.Position
	}
	if updated.Permissions != 0 {
		merged.Permissions = updated.Permissions
	}
	if len(updated.PermissionNames) > 0 {
		merged.PermissionNames = updated.PermissionNames
	}
	return merged
}

func mergeChannel(
	current discord.GuildChannel,
	updated discord.GuildChannel,
	req discord.ChannelUpdateRequest,
) discord.GuildChannel {
	merged := current
	if updated.ID != "" {
		merged.ID = updated.ID
	}
	if updated.GuildID != "" {
		merged.GuildID = updated.GuildID
	}
	if updated.Type != "" {
		merged.Type = updated.Type
	}
	if req.Name != nil {
		merged.Name = *req.Name
	}
	if req.Topic != nil {
		merged.Topic = *req.Topic
	}
	if req.ParentID != nil {
		merged.ParentID = *req.ParentID
	}
	if updated.Position != 0 {
		merged.Position = updated.Position
	}
	merged.NSFW = updated.NSFW
	if len(updated.PermissionOverwrites) > 0 {
		merged.PermissionOverwrites = updated.PermissionOverwrites
	}
	return merged
}

func findRoleOverwrite(overwrites []discord.PermissionOverwrite, targetID string) (discord.PermissionOverwrite, bool) {
	for _, overwrite := range overwrites {
		if overwrite.TargetType == "role" && overwrite.TargetID == targetID {
			return overwrite, true
		}
	}
	return discord.PermissionOverwrite{}, false
}

func overwriteMatchesSpec(current discord.PermissionOverwrite, desired OverwriteSpec) bool {
	currentAllow := append([]string(nil), current.AllowNames...)
	currentDeny := append([]string(nil), current.DenyNames...)
	desiredAllow := append([]string(nil), desired.Allow...)
	desiredDeny := append([]string(nil), desired.Deny...)

	sort.Strings(currentAllow)
	sort.Strings(currentDeny)
	sort.Strings(desiredAllow)
	sort.Strings(desiredDeny)

	return slices.Equal(currentAllow, desiredAllow) && slices.Equal(currentDeny, desiredDeny)
}

func upsertOverwrite(
	overwrites []discord.PermissionOverwrite,
	overwrite discord.PermissionOverwrite,
) []discord.PermissionOverwrite {
	for idx := range overwrites {
		if overwrites[idx].TargetType == overwrite.TargetType && overwrites[idx].TargetID == overwrite.TargetID {
			overwrites[idx] = overwrite
			return overwrites
		}
	}
	return append(overwrites, overwrite)
}

func strconvQuote(value string) string {
	return `"` + value + `"`
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

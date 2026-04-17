package scaffold

import (
	"context"
	"fmt"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Plan Helpers ---

// BuildPlan loads live state and calculates a scaffold plan.
func BuildPlan(ctx context.Context, manager discord.Manager, spec Spec) (Plan, error) {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return Plan{}, err
	}

	return Diff(spec, state), nil
}

// Apply applies a scaffold spec to Discord and returns the resulting plan.
func Apply(ctx context.Context, manager discord.Manager, spec Spec) (Plan, error) {
	plan, err := BuildPlan(ctx, manager, spec)
	if err != nil {
		return Plan{}, err
	}

	if err := applyRoles(ctx, manager, spec); err != nil {
		return Plan{}, err
	}
	if err := applyCategories(ctx, manager, spec); err != nil {
		return Plan{}, err
	}
	if err := applyChannels(ctx, manager, spec); err != nil {
		return Plan{}, err
	}
	if err := deleteExtraChannels(ctx, manager, spec); err != nil {
		return Plan{}, err
	}
	if err := deleteExtraRoles(ctx, manager, spec); err != nil {
		return Plan{}, err
	}

	return plan, nil
}

func applyRoles(ctx context.Context, manager discord.Manager, spec Spec) error {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return err
	}

	roleIndex := make(map[string]discord.GuildRole, len(state.Roles))
	for _, role := range state.Roles {
		roleIndex[role.Name] = role
	}

	for _, role := range spec.Roles {
		color, hasColor := parseHexColor(role.Color)
		current, ok := roleIndex[role.Name]
		if !ok {
			req := discord.RoleCreateRequest{
				GuildID:     spec.Guild.ID,
				Name:        role.Name,
				Hoist:       boolPointer(role.Hoist),
				Mentionable: boolPointer(role.Mentionable),
			}
			if hasColor {
				req.Color = intPointer(color)
			}
			if _, err := manager.CreateRole(ctx, req); err != nil {
				return err
			}
			continue
		}

		req := discord.RoleUpdateRequest{
			GuildID: spec.Guild.ID,
			RoleID:  current.ID,
		}
		changed := false
		if hasColor && current.Color != color {
			req.Color = intPointer(color)
			changed = true
		}
		if current.Hoist != role.Hoist {
			req.Hoist = boolPointer(role.Hoist)
			changed = true
		}
		if current.Mentionable != role.Mentionable {
			req.Mentionable = boolPointer(role.Mentionable)
			changed = true
		}
		if changed {
			if _, err := manager.UpdateRole(ctx, req); err != nil {
				return err
			}
		}
	}

	return nil
}

func applyCategories(ctx context.Context, manager discord.Manager, spec Spec) error {
	return applyChannelGroup(ctx, manager, spec, spec.Categories)
}

func applyChannels(ctx context.Context, manager discord.Manager, spec Spec) error {
	return applyChannelGroup(ctx, manager, spec, spec.Channels)
}

func applyChannelGroup(ctx context.Context, manager discord.Manager, spec Spec, channels []ChannelSpec) error {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return err
	}

	channelIndex := make(map[string]discord.GuildChannel, len(state.Channels))
	for _, channel := range state.Channels {
		channelIndex[channelKey(channel.Type, channel.Name)] = channel
	}

	parentIDs := make(map[string]string, len(state.Channels))
	for _, channel := range state.Channels {
		if channel.Type == "category" {
			parentIDs[channel.Name] = channel.ID
		}
	}

	roleIndex := make(map[string]discord.GuildRole, len(state.Roles))
	for _, role := range state.Roles {
		roleIndex[role.Name] = role
	}

	for _, channel := range channels {
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
			created, err := manager.CreateChannel(ctx, discord.ChannelCreateRequest{
				GuildID:  spec.Guild.ID,
				Name:     channel.Name,
				Type:     channel.Type,
				ParentID: parentID,
				Topic:    channel.Topic,
			})
			if err != nil {
				return err
			}
			current = created
			channelIndex[key] = created
			if created.Type == "category" {
				parentIDs[created.Name] = created.ID
			}
		} else {
			req := discord.ChannelUpdateRequest{ID: current.ID}
			changed := false
			if channel.Type != "category" && strings.TrimSpace(channel.Topic) != strings.TrimSpace(current.Topic) {
				req.Topic = stringPointer(channel.Topic)
				changed = true
			}
			if strings.TrimSpace(parentID) != strings.TrimSpace(current.ParentID) {
				req.ParentID = stringPointer(parentID)
				changed = true
			}
			if changed {
				updated, err := manager.UpdateChannel(ctx, req)
				if err != nil {
					return err
				}
				current = updated
				channelIndex[key] = updated
			}
		}

		if err := applyOverwrites(ctx, manager, current, channel.Overwrites, roleIndex); err != nil {
			return err
		}
	}

	return nil
}

func applyOverwrites(
	ctx context.Context,
	manager discord.Manager,
	channel discord.GuildChannel,
	desired []OverwriteSpec,
	roleIndex map[string]discord.GuildRole,
) error {
	desiredRoles := make(map[string]OverwriteSpec, len(desired))
	for _, overwrite := range desired {
		desiredRoles[overwrite.Role] = overwrite
	}

	idToRoleName := make(map[string]string, len(roleIndex))
	for name, role := range roleIndex {
		idToRoleName[role.ID] = name
	}

	for _, overwrite := range channel.PermissionOverwrites {
		if overwrite.TargetType != "role" {
			continue
		}
		name, ok := idToRoleName[overwrite.TargetID]
		if !ok {
			continue
		}
		if _, ok := desiredRoles[name]; ok {
			continue
		}
		if err := manager.RemoveChannelPermission(ctx, discord.ChannelPermissionRemoveRequest{
			ChannelID:  channel.ID,
			TargetID:   overwrite.TargetID,
			TargetType: "role",
		}); err != nil {
			return err
		}
	}

	for _, overwrite := range desired {
		role, ok := roleIndex[overwrite.Role]
		if !ok {
			return fmt.Errorf("scaffold overwrite role %q not found", overwrite.Role)
		}
		if err := manager.SetChannelPermission(ctx, discord.ChannelPermissionSetRequest{
			ChannelID:  channel.ID,
			TargetID:   role.ID,
			TargetType: "role",
			Allow:      overwrite.Allow,
			Deny:       overwrite.Deny,
		}); err != nil {
			return err
		}
	}

	return nil
}

func deleteExtraChannels(ctx context.Context, manager discord.Manager, spec Spec) error {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return err
	}

	desired := make(map[string]struct{}, len(spec.Categories)+len(spec.Channels))
	for _, channel := range desiredChannelSpecs(spec) {
		desired[channelKey(channel.Type, channel.Name)] = struct{}{}
	}

	deleteGroup := func(channelType string) error {
		for _, channel := range state.Channels {
			if channel.Type != channelType {
				continue
			}
			if _, ok := desired[channelKey(channel.Type, channel.Name)]; ok {
				continue
			}
			if err := manager.DeleteChannel(ctx, channel.ID); err != nil {
				return err
			}
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

func deleteExtraRoles(ctx context.Context, manager discord.Manager, spec Spec) error {
	state, err := LoadState(ctx, manager, spec.Guild.ID)
	if err != nil {
		return err
	}

	desired := make(map[string]struct{}, len(spec.Roles))
	for _, role := range spec.Roles {
		desired[role.Name] = struct{}{}
	}

	for _, role := range state.Roles {
		if role.Name == "@everyone" {
			continue
		}
		if _, ok := desired[role.Name]; ok {
			continue
		}
		if err := manager.DeleteRole(ctx, discord.RoleDeleteRequest{
			GuildID: spec.Guild.ID,
			RoleID:  role.ID,
		}); err != nil {
			return err
		}
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

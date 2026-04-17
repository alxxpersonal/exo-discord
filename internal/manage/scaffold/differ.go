package scaffold

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/discord"
)

// --- Plan Types ---

// State stores the live guild state used for diffing.
type State struct {
	GuildID  string                 `json:"guild_id"`
	Roles    []discord.GuildRole    `json:"roles,omitempty"`
	Channels []discord.GuildChannel `json:"channels,omitempty"`
}

// Plan stores the scaffold delta plan.
type Plan struct {
	GuildID string   `json:"guild_id"`
	Changes []Change `json:"changes,omitempty"`
}

// Change stores a single planned mutation.
type Change struct {
	Action string   `json:"action"`
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Parent string   `json:"parent,omitempty"`
	Fields []string `json:"fields,omitempty"`
}

// --- State Loading ---

// LoadState loads the current guild roles and channels from Discord.
func LoadState(ctx context.Context, manager discord.Manager, guildID string) (State, error) {
	roles, err := manager.ListRoles(ctx, guildID)
	if err != nil {
		return State{}, err
	}

	channels, err := manager.ListChannels(ctx, guildID)
	if err != nil {
		return State{}, err
	}

	return State{
		GuildID:  guildID,
		Roles:    roles,
		Channels: channels,
	}, nil
}

// --- Diffing ---

// Diff calculates the delta between a scaffold spec and live state.
func Diff(spec Spec, state State) Plan {
	plan := Plan{
		GuildID: spec.Guild.ID,
		Changes: make([]Change, 0),
	}

	roleIndex := make(map[string]discord.GuildRole, len(state.Roles))
	for _, role := range state.Roles {
		roleIndex[role.Name] = role
	}

	desiredRoles := make([]RoleSpec, 0, len(spec.Roles))
	desiredRoleNames := make(map[string]struct{}, len(spec.Roles))
	for _, role := range spec.Roles {
		desiredRoles = append(desiredRoles, role)
		desiredRoleNames[role.Name] = struct{}{}
	}
	sort.Slice(desiredRoles, func(i, j int) bool {
		return desiredRoles[i].Name < desiredRoles[j].Name
	})

	for _, role := range desiredRoles {
		current, ok := roleIndex[role.Name]
		if !ok {
			plan.Changes = append(plan.Changes, Change{
				Action: "create",
				Kind:   "role",
				Name:   role.Name,
			})
			continue
		}

		fields := make([]string, 0, 3)
		if color, ok := parseHexColor(role.Color); ok && color != current.Color {
			fields = append(fields, "color")
		}
		if role.Hoist != current.Hoist {
			fields = append(fields, "hoist")
		}
		if role.Mentionable != current.Mentionable {
			fields = append(fields, "mentionable")
		}
		if len(fields) > 0 {
			plan.Changes = append(plan.Changes, Change{
				Action: "update",
				Kind:   "role",
				Name:   role.Name,
				Fields: fields,
			})
		}
	}

	extraRoleNames := make([]string, 0)
	for _, role := range state.Roles {
		if role.Name == "@everyone" {
			continue
		}
		if _, ok := desiredRoleNames[role.Name]; ok {
			continue
		}
		extraRoleNames = append(extraRoleNames, role.Name)
	}
	sort.Strings(extraRoleNames)
	for _, name := range extraRoleNames {
		plan.Changes = append(plan.Changes, Change{
			Action: "delete",
			Kind:   "role",
			Name:   name,
		})
	}

	desiredChannels := desiredChannelSpecs(spec)
	channelIndex := make(map[string]discord.GuildChannel, len(state.Channels))
	parentNames := make(map[string]string, len(state.Channels))
	for _, channel := range state.Channels {
		channelIndex[channelKey(channel.Type, channel.Name)] = channel
	}
	for _, channel := range state.Channels {
		parentNames[channel.ID] = parentName(channel.ParentID, state.Channels)
	}

	desiredChannelNames := make(map[string]struct{}, len(desiredChannels))
	for _, channel := range desiredChannels {
		key := channelKey(channel.Type, channel.Name)
		desiredChannelNames[key] = struct{}{}
		current, ok := channelIndex[key]
		if !ok {
			plan.Changes = append(plan.Changes, Change{
				Action: "create",
				Kind:   "channel",
				Name:   channel.Name,
				Parent: channel.Parent,
				Fields: channelFields(channel),
			})
			continue
		}

		fields := make([]string, 0, 3)
		if strings.TrimSpace(channel.Topic) != strings.TrimSpace(current.Topic) && channel.Type != "category" {
			fields = append(fields, "topic")
		}
		if strings.TrimSpace(channel.Parent) != strings.TrimSpace(parentNames[current.ID]) {
			fields = append(fields, "parent")
		}
		if overwriteDiffers(channel.Overwrites, current.PermissionOverwrites, state.Roles) {
			fields = append(fields, "overwrites")
		}
		if len(fields) > 0 {
			plan.Changes = append(plan.Changes, Change{
				Action: "update",
				Kind:   "channel",
				Name:   channel.Name,
				Parent: channel.Parent,
				Fields: fields,
			})
		}
	}

	extraChannels := make([]discord.GuildChannel, 0)
	for _, channel := range state.Channels {
		key := channelKey(channel.Type, channel.Name)
		if _, ok := desiredChannelNames[key]; ok {
			continue
		}
		extraChannels = append(extraChannels, channel)
	}
	sort.Slice(extraChannels, func(i, j int) bool {
		if extraChannels[i].Type != extraChannels[j].Type {
			if extraChannels[i].Type == "category" {
				return false
			}
			if extraChannels[j].Type == "category" {
				return true
			}
			return extraChannels[i].Type < extraChannels[j].Type
		}
		return extraChannels[i].Name < extraChannels[j].Name
	})
	for _, channel := range extraChannels {
		plan.Changes = append(plan.Changes, Change{
			Action: "delete",
			Kind:   "channel",
			Name:   channel.Name,
			Parent: parentNames[channel.ID],
		})
	}

	return plan
}

func desiredChannelSpecs(spec Spec) []ChannelSpec {
	channels := make([]ChannelSpec, 0, len(spec.Categories)+len(spec.Channels))
	channels = append(channels, spec.Categories...)
	channels = append(channels, spec.Channels...)

	sort.Slice(channels, func(i, j int) bool {
		if channels[i].Type != channels[j].Type {
			if channels[i].Type == "category" {
				return true
			}
			if channels[j].Type == "category" {
				return false
			}
			return channels[i].Type < channels[j].Type
		}
		return channels[i].Name < channels[j].Name
	})

	return channels
}

func channelFields(channel ChannelSpec) []string {
	fields := []string{"type"}
	if strings.TrimSpace(channel.Topic) != "" && channel.Type != "category" {
		fields = append(fields, "topic")
	}
	if strings.TrimSpace(channel.Parent) != "" {
		fields = append(fields, "parent")
	}
	if len(channel.Overwrites) > 0 {
		fields = append(fields, "overwrites")
	}
	return fields
}

func overwriteDiffers(
	desired []OverwriteSpec,
	current []discord.PermissionOverwrite,
	roles []discord.GuildRole,
) bool {
	roleNames := make(map[string]string, len(roles))
	for _, role := range roles {
		roleNames[role.ID] = role.Name
	}

	currentNames := make([]string, 0, len(current))
	for _, overwrite := range current {
		if overwrite.TargetType != "role" {
			continue
		}
		name, ok := roleNames[overwrite.TargetID]
		if !ok {
			continue
		}
		currentNames = append(currentNames, name+":"+strings.Join(overwrite.AllowNames, ",")+":"+strings.Join(overwrite.DenyNames, ","))
	}

	desiredNames := make([]string, 0, len(desired))
	for _, overwrite := range desired {
		allow := append([]string(nil), overwrite.Allow...)
		deny := append([]string(nil), overwrite.Deny...)
		sort.Strings(allow)
		sort.Strings(deny)
		desiredNames = append(desiredNames, overwrite.Role+":"+strings.Join(allow, ",")+":"+strings.Join(deny, ","))
	}

	sort.Strings(currentNames)
	sort.Strings(desiredNames)
	return !slices.Equal(currentNames, desiredNames)
}

func channelKey(channelType string, name string) string {
	return strings.TrimSpace(channelType) + ":" + strings.TrimSpace(name)
}

func parentName(parentID string, channels []discord.GuildChannel) string {
	if strings.TrimSpace(parentID) == "" {
		return ""
	}
	for _, channel := range channels {
		if channel.ID == parentID {
			return channel.Name
		}
	}
	return ""
}

func parseHexColor(value string) (int, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if trimmed == "" {
		return 0, false
	}
	if len(trimmed) != 6 {
		return 0, false
	}

	var color int
	for _, r := range trimmed {
		color <<= 4
		switch {
		case r >= '0' && r <= '9':
			color += int(r - '0')
		case r >= 'a' && r <= 'f':
			color += int(r-'a') + 10
		case r >= 'A' && r <= 'F':
			color += int(r-'A') + 10
		default:
			return 0, false
		}
	}
	return color, true
}

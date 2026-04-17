package scaffold

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

// --- Schema Types ---

// Spec stores the declarative Discord guild specification.
type Spec struct {
	Guild      GuildSpec     `json:"guild" yaml:"guild"`
	Roles      []RoleSpec    `json:"roles,omitempty" yaml:"roles,omitempty"`
	Categories []ChannelSpec `json:"categories,omitempty" yaml:"categories,omitempty"`
	Channels   []ChannelSpec `json:"channels,omitempty" yaml:"channels,omitempty"`
}

// GuildSpec stores top-level guild identity metadata.
type GuildSpec struct {
	ID   string `json:"id" yaml:"id"`
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
}

// RoleSpec stores declarative role state.
type RoleSpec struct {
	Name        string `json:"name" yaml:"name"`
	Color       string `json:"color,omitempty" yaml:"color,omitempty"`
	Hoist       bool   `json:"hoist,omitempty" yaml:"hoist,omitempty"`
	Mentionable bool   `json:"mentionable,omitempty" yaml:"mentionable,omitempty"`
}

// ChannelSpec stores declarative channel state.
type ChannelSpec struct {
	Name       string          `json:"name" yaml:"name"`
	Type       string          `json:"type,omitempty" yaml:"type,omitempty"`
	Topic      string          `json:"topic,omitempty" yaml:"topic,omitempty"`
	Parent     string          `json:"parent,omitempty" yaml:"parent,omitempty"`
	Overwrites []OverwriteSpec `json:"overwrites,omitempty" yaml:"overwrites,omitempty"`
}

// OverwriteSpec stores declarative role overwrite state.
type OverwriteSpec struct {
	Role  string   `json:"role" yaml:"role"`
	Allow []string `json:"allow,omitempty" yaml:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty" yaml:"deny,omitempty"`
}

// --- Load Helpers ---

// LoadSpec loads a scaffold spec from YAML on disk.
func LoadSpec(path string) (Spec, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return Spec{}, fmt.Errorf("read scaffold spec: %w", err)
	}

	var spec Spec
	if err := yaml.Unmarshal(payload, &spec); err != nil {
		return Spec{}, fmt.Errorf("decode scaffold spec: %w", err)
	}

	normalizeSpec(&spec)
	if strings.TrimSpace(spec.Guild.ID) == "" {
		return Spec{}, fmt.Errorf("scaffold guild id must not be empty")
	}

	return spec, nil
}

func normalizeSpec(spec *Spec) {
	for idx := range spec.Categories {
		spec.Categories[idx].Type = "category"
	}
	for idx := range spec.Channels {
		if strings.TrimSpace(spec.Channels[idx].Type) == "" {
			spec.Channels[idx].Type = "text"
		}
	}
}

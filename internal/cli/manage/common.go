package managecmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- Types ---

// Environment stores the dependencies used by the manage command tree.
type Environment struct {
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	ResolveConfig  func() (config.ResolvedConfig, error)
	CommandContext func() context.Context
	NewManager     func(config.ResolvedConfig) (discord.Manager, error)
}

// --- IO Helpers ---

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// --- Manager Helpers ---

func withManager(env Environment, fn func(context.Context, discord.Manager) error) error {
	resolved, err := env.ResolveConfig()
	if err != nil {
		return err
	}

	manager, err := env.NewManager(resolved)
	if err != nil {
		return err
	}

	return fn(env.CommandContext(), manager)
}

func requireYes(yes bool, action string) error {
	if yes {
		return nil
	}
	return fmt.Errorf("%s requires --yes", action)
}

func stringPointer(value string) *string {
	return &value
}

func parseJSON(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	if !json.Valid([]byte(value)) {
		return nil, fmt.Errorf("invalid json")
	}
	return []byte(value), nil
}

func markRequired(cmd *cobra.Command, name string) {
	_ = cmd.MarkFlagRequired(name)
}

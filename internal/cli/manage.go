package cli

import (
	managecmd "github.com/alxxpersonal/exo-discord/internal/cli/manage"
	"github.com/spf13/cobra"
)

// --- Manage Commands ---

func newManageCommand(env Environment) *cobra.Command {
	return managecmd.NewCommand(managecmd.Environment{
		Stdin:          env.Stdin,
		Stdout:         env.Stdout,
		Stderr:         env.Stderr,
		ResolveConfig:  env.resolveConfig,
		CommandContext: env.commandContext,
		NewManager:     env.NewManager,
	})
}

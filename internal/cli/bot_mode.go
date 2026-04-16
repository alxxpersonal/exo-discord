package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// --- Bot Mode Command ---

func newBotModeCommand(env Environment) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:     "bot-mode",
		Short:   "Run the Discord bot gateway loop",
		Example: "exo-discord bot-mode --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			if dryRun {
				return writeJSON(cmd.OutOrStdout(), modeDryRunResult{
					OK:         true,
					Command:    "bot-mode",
					DryRun:     true,
					ConfigPath: resolved.ConfigPath,
					Mode:       string(resolved.Config.Mode),
					HookKind:   string(resolved.Config.Hook.Kind),
				})
			}

			if env.BotModeRunner == nil {
				return errors.New("bot-mode runtime is not implemented")
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			return env.BotModeRunner.Run(env.commandContext(), botModeRequest{
				Config:  resolved,
				Session: session,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate config and print the planned bot mode")
	return cmd
}

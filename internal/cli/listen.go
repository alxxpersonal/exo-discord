package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// --- Types ---

type modeDryRunResult struct {
	OK         bool   `json:"ok"`
	Command    string `json:"command"`
	DryRun     bool   `json:"dry_run"`
	ConfigPath string `json:"config_path"`
	Mode       string `json:"mode"`
	HookKind   string `json:"hook_kind"`
}

// --- Listen Command ---

func newListenCommand(env Environment) *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:     "listen",
		Short:   "Print inbound Discord envelopes to stdout",
		Example: "exo-discord listen --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			if dryRun {
				return writeJSON(cmd.OutOrStdout(), modeDryRunResult{
					OK:         true,
					Command:    "listen",
					DryRun:     true,
					ConfigPath: resolved.ConfigPath,
					Mode:       string(resolved.Config.Mode),
					HookKind:   string(resolved.Config.Hook.Kind),
				})
			}

			if env.ListenRunner == nil {
				return errors.New("listen runtime is not implemented")
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			return env.ListenRunner.Run(env.commandContext(), listenRequest{
				Config:  resolved,
				Session: session,
				Stdout:  cmd.OutOrStdout(),
				Stderr:  cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate config and print the planned listen mode")
	return cmd
}

package cli

import (
	"fmt"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/spf13/cobra"
)

// --- Init Command ---

func newInitCommand(env Environment) *cobra.Command {
	var force bool
	var project bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a starter config file",
		RunE: func(cmd *cobra.Command, args []string) error {
			var (
				path string
				err  error
			)

			if project {
				path, err = config.WriteProjectConfig(env.StartDir, env.HomeDir, force)
			} else {
				path, err = config.WriteHomeConfig(env.HomeDir, force)
			}
			if err != nil {
				return err
			}

			_, err = fmt.Fprintln(cmd.OutOrStdout(), path)
			return err
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config")
	cmd.Flags().BoolVar(&project, "project", false, "write .exo-discord in the current directory")

	return cmd
}

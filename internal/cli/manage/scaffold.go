package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	scaffoldpkg "github.com/alxxpersonal/exo-discord/internal/manage/scaffold"
	"github.com/spf13/cobra"
)

// --- Scaffold Commands ---

func newScaffoldCommand(env Environment) *cobra.Command {
	var (
		from  string
		apply bool
		yes   bool
	)

	cmd := &cobra.Command{
		Use:   "scaffold",
		Short: "Diff or apply declarative Discord guild state from YAML",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := scaffoldpkg.LoadSpec(from)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if !apply {
					plan, err := scaffoldpkg.BuildPlan(ctx, manager, spec)
					if err != nil {
						return err
					}
					return writeJSON(cmd.OutOrStdout(), plan)
				}

				if err := requireYes(yes, "scaffold apply"); err != nil {
					return err
				}

				plan, err := scaffoldpkg.Apply(ctx, manager, spec)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"ok":    true,
					"apply": true,
					"plan":  plan,
				})
			})
		},
	}

	cmd.Flags().StringVar(&from, "from", "", "path to scaffold yaml file")
	cmd.Flags().BoolVar(&apply, "apply", false, "apply the scaffold delta")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm scaffold apply")
	markRequired(cmd, "from")
	return cmd
}

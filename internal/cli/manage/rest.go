package managecmd

import (
	"context"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	"github.com/spf13/cobra"
)

// --- REST Commands ---

func newRESTCommand(env Environment) *cobra.Command {
	var body string

	cmd := &cobra.Command{
		Use:   "rest <METHOD> <PATH>",
		Short: "Execute a raw Discord REST request",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			payload, err := parseJSON(body)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				response, err := manager.REST(ctx, discord.RESTRequest{
					Method: args[0],
					Path:   args[1],
					Body:   payload,
				})
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), response)
			})
		},
	}

	cmd.Flags().StringVar(&body, "body", "", "json request body")
	return cmd
}

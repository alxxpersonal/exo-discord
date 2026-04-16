package cli

import (
	"errors"
	"fmt"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/spf13/cobra"
)

// --- MCP Commands ---

func newMCPCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Run MCP-related commands",
	}

	cmd.AddCommand(newMCPServeCommand(env))
	return cmd
}

func newMCPServeCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:     "serve",
		Short:   "Serve the MCP control surface over stdio",
		Example: "exo-discord mcp serve",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}
			if !resolved.Config.MCPEnabled {
				return errors.New("mcp is disabled")
			}
			if resolved.Config.MCP.Transport != config.MCPTransportStdio {
				return fmt.Errorf("mcp %s transport is not implemented", resolved.Config.MCP.Transport)
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			ctx := env.commandContext()
			if err := session.Open(ctx); err != nil {
				return err
			}
			defer func() {
				_ = session.Close(ctx)
			}()

			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "serving mcp over stdio")
			return env.NewMCPServer(session).Run(ctx)
		},
	}
}

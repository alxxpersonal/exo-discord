package managecmd

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/alxxpersonal/exo-discord/internal/discord"
	runtimeexec "github.com/alxxpersonal/exo-discord/internal/manage/runtimeexec"
	"github.com/spf13/cobra"
)

// --- Exec Commands ---

func newExecCommand(env Environment) *cobra.Command {
	var (
		filePath string
		expr     string
	)

	cmd := &cobra.Command{
		Use:   "exec",
		Short: "Execute runtime Go code against the manager surface through yaegi",
		RunE: func(cmd *cobra.Command, args []string) error {
			source, err := loadExecSource(env.Stdin, filePath, expr)
			if err != nil {
				return err
			}

			return withManager(env, func(ctx context.Context, manager discord.Manager) error {
				if err := runtimeexec.ExecuteSource(ctx, manager, source); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), map[string]any{"ok": true})
			})
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "path to a Go source file containing Run")
	cmd.Flags().StringVar(&expr, "expr", "", "inline Go statements used as the Run function body")
	return cmd
}

func loadExecSource(stdin io.Reader, filePath string, expr string) (string, error) {
	switch {
	case filePath != "" && expr != "":
		return "", fmt.Errorf("exec accepts either --file or --expr")
	case filePath != "":
		payload, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("read exec file: %w", err)
		}
		return string(payload), nil
	case expr != "":
		return "package main\nfunc Run(client *ManagerClient) error {\n" + expr + "\n}\n", nil
	default:
		payload, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read exec stdin: %w", err)
		}
		if len(payload) == 0 {
			return "", fmt.Errorf("exec requires --file, --expr, or stdin source")
		}
		return string(payload), nil
	}
}

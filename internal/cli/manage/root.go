package managecmd

import "github.com/spf13/cobra"

// --- Commands ---

// NewCommand creates the manage command tree.
func NewCommand(env Environment) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manage",
		Short: "Manage Discord guild state through the bot token",
	}

	if env.Stdin != nil {
		cmd.SetIn(env.Stdin)
	}
	if env.Stdout != nil {
		cmd.SetOut(env.Stdout)
	}
	if env.Stderr != nil {
		cmd.SetErr(env.Stderr)
	}

	cmd.AddCommand(newGuildCommand(env))
	cmd.AddCommand(newChannelCommand(env))
	cmd.AddCommand(newRoleCommand(env))
	cmd.AddCommand(newMemberCommand(env))
	cmd.AddCommand(newMessageCommand(env))
	cmd.AddCommand(newEmbedCommand(env))
	cmd.AddCommand(newInteractionsCommand(env))
	cmd.AddCommand(newRESTCommand(env))
	cmd.AddCommand(newScaffoldCommand(env))
	cmd.AddCommand(newExecCommand(env))

	return cmd
}

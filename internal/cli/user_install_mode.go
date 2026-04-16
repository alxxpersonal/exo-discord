package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// --- User Install Mode Command ---

func newUserInstallModeCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "user-install-mode",
		Short: "Reserved for future official user-installed app mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("user_install mode is not implemented")
		},
	}
}

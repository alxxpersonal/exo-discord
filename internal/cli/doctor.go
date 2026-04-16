package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/spf13/cobra"
)

// --- Types ---

type doctorReport struct {
	ConfigPath      string `json:"config_path"`
	Mode            string `json:"mode"`
	BotTokenPresent bool   `json:"bot_token_present"`
	MCPEnabled      bool   `json:"mcp_enabled"`
	CLIEnabled      bool   `json:"cli_enabled"`
	HookKind        string `json:"hook_kind"`
	ConfigPerms     string `json:"config_perms"`
	StateDirPath    string `json:"state_dir_path"`
	StateDirStatus  string `json:"state_dir_status"`
	AccessStatePath string `json:"access_state_path"`
	AuditLogPath    string `json:"audit_log_path"`
	InboxDirPath    string `json:"inbox_dir_path"`
	RequireMention  bool   `json:"require_mention"`
	AllowedUsers    int    `json:"allowed_users"`
	AllowedChannels int    `json:"allowed_channels"`
	AllowedRoles    int    `json:"allowed_roles"`
}

// --- Doctor Command ---

func newDoctorCommand(env Environment) *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Validate config discovery and local state assumptions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := config.DiscoverFrom(env.StartDir, env.HomeDir)
			if err != nil {
				return err
			}

			report := doctorReport{
				ConfigPath:      resolved.ConfigPath,
				Mode:            string(resolved.Config.Mode),
				BotTokenPresent: resolved.Config.BotToken != "",
				MCPEnabled:      resolved.Config.MCPEnabled,
				CLIEnabled:      resolved.Config.CLIEnabled,
				HookKind:        string(resolved.Config.Hook.Kind),
				ConfigPerms:     "0600",
				StateDirPath:    resolved.HomeStateDir,
				StateDirStatus:  stateDirStatus(resolved.HomeStateDir),
				AccessStatePath: resolved.AccessStatePath,
				AuditLogPath:    resolved.AuditLogPath,
				InboxDirPath:    resolved.InboxDirPath,
				RequireMention:  resolved.Config.RequireMention,
				AllowedUsers:    len(resolved.Config.AllowedUserIDs),
				AllowedChannels: len(resolved.Config.AllowedChannelIDs),
				AllowedRoles:    len(resolved.Config.AllowedRoleIDs),
			}

			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}

			_, err = fmt.Fprintf(
				cmd.OutOrStdout(),
				"config_path: %s\nmode: %s\nbot_token_present: %t\nstate_dir_status: %s\nhook_kind: %s\n",
				report.ConfigPath,
				report.Mode,
				report.BotTokenPresent,
				report.StateDirStatus,
				report.HookKind,
			)
			return err
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "print a machine-readable report")

	return cmd
}

// --- Helpers ---

func stateDirStatus(path string) string {
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return "missing"
	case err != nil:
		return "error"
	case !info.IsDir():
		return "invalid"
	case info.Mode().Perm() != 0o700:
		return "invalid"
	default:
		return "ok"
	}
}

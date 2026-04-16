package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/spf13/cobra"
)

// --- Types ---

type configureResult struct {
	OK         bool     `json:"ok"`
	ConfigPath string   `json:"config_path"`
	Updated    []string `json:"updated"`
}

// --- Configure Command ---

func newConfigureCommand(env Environment) *cobra.Command {
	var (
		mode              string
		botToken          string
		botTokenStdin     bool
		mcpEnabled        bool
		cliEnabled        bool
		requireMention    bool
		pairingEnabled    bool
		hookKind          string
		hookHTTPURL       string
		hookStdioCommand  []string
		mcpTransport      string
		mcpListenAddr     string
		downloadsDir      string
		maxAttachmentSize int64
		presence          string
		activityType      string
		activityText      string
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Update config keys in the active config file",
		Example: strings.Join([]string{
			"exo-discord configure --bot-token \"$DISCORD_BOT_TOKEN\"",
			"exo-discord configure --hook-kind http --hook-http-url http://127.0.0.1:8787/hook",
		}, "\n"),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("bot-token") && cmd.Flags().Changed("bot-token-stdin") {
				return errors.New("bot token must come from exactly one source")
			}

			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			cfg := resolved.Config
			updated := make([]string, 0, 12)

			if cmd.Flags().Changed("mode") {
				cfg.Mode = config.Mode(strings.TrimSpace(mode))
				updated = append(updated, "mode")
			}

			if cmd.Flags().Changed("bot-token") {
				cfg.BotToken = strings.TrimSpace(botToken)
				updated = append(updated, "bot_token")
			}

			if cmd.Flags().Changed("bot-token-stdin") {
				token, err := readTokenFromReader(cmd.InOrStdin())
				if err != nil {
					return err
				}
				cfg.BotToken = token
				updated = append(updated, "bot_token")
			}

			if cmd.Flags().Changed("mcp-enabled") {
				cfg.MCPEnabled = mcpEnabled
				updated = append(updated, "mcp_enabled")
			}

			if cmd.Flags().Changed("cli-enabled") {
				cfg.CLIEnabled = cliEnabled
				updated = append(updated, "cli_enabled")
			}

			if cmd.Flags().Changed("require-mention") {
				cfg.RequireMention = requireMention
				updated = append(updated, "require_mention")
			}

			if cmd.Flags().Changed("pairing-enabled") {
				cfg.Pairing.Enabled = pairingEnabled
				updated = append(updated, "pairing.enabled")
			}

			if cmd.Flags().Changed("hook-kind") {
				cfg.Hook.Kind = config.HookKind(strings.TrimSpace(hookKind))
				updated = append(updated, "hook.kind")
			}

			if cmd.Flags().Changed("hook-http-url") {
				cfg.Hook.HTTP.URL = strings.TrimSpace(hookHTTPURL)
				updated = append(updated, "hook.http.url")
			}

			if cmd.Flags().Changed("hook-stdio-command") {
				cfg.Hook.Stdio.Command = compactValues(hookStdioCommand)
				updated = append(updated, "hook.stdio.command")
			}

			if cmd.Flags().Changed("mcp-transport") {
				cfg.MCP.Transport = config.MCPTransport(strings.TrimSpace(mcpTransport))
				updated = append(updated, "mcp.transport")
			}

			if cmd.Flags().Changed("mcp-listen-addr") {
				cfg.MCP.ListenAddr = strings.TrimSpace(mcpListenAddr)
				updated = append(updated, "mcp.listen_addr")
			}

			if cmd.Flags().Changed("downloads-dir") {
				cfg.Downloads.Dir = strings.TrimSpace(downloadsDir)
				updated = append(updated, "downloads.dir")
			}

			if cmd.Flags().Changed("max-attachment-bytes") {
				cfg.Downloads.MaxAttachmentBytes = maxAttachmentSize
				updated = append(updated, "downloads.max_attachment_bytes")
			}

			if cmd.Flags().Changed("presence") {
				cfg.Status.Presence = config.Presence(strings.TrimSpace(presence))
				updated = append(updated, "status.presence")
			}

			if cmd.Flags().Changed("activity-type") {
				cfg.Status.ActivityType = config.ActivityType(strings.TrimSpace(activityType))
				updated = append(updated, "status.activity_type")
			}

			if cmd.Flags().Changed("activity-text") {
				cfg.Status.ActivityText = activityText
				updated = append(updated, "status.activity_text")
			}

			if len(updated) == 0 {
				return errors.New("no config updates requested")
			}

			if err := saveConfig(resolved.ConfigPath, cfg); err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), configureResult{
				OK:         true,
				ConfigPath: resolved.ConfigPath,
				Updated:    updated,
			})
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "", "set mode to bot or user_install")
	cmd.Flags().StringVar(&botToken, "bot-token", "", "set the bot token")
	cmd.Flags().BoolVar(&botTokenStdin, "bot-token-stdin", false, "read the bot token from stdin")
	cmd.Flags().BoolVar(&mcpEnabled, "mcp-enabled", false, "enable or disable mcp")
	cmd.Flags().BoolVar(&cliEnabled, "cli-enabled", false, "enable or disable cli")
	cmd.Flags().BoolVar(&requireMention, "require-mention", false, "require mention or reply in guild channels")
	cmd.Flags().BoolVar(&pairingEnabled, "pairing-enabled", false, "enable or disable dm pairing")
	cmd.Flags().StringVar(&hookKind, "hook-kind", "", "set hook kind to none, http, or stdio")
	cmd.Flags().StringVar(&hookHTTPURL, "hook-http-url", "", "set the hook http url")
	cmd.Flags().StringSliceVar(&hookStdioCommand, "hook-stdio-command", nil, "set the hook stdio command")
	cmd.Flags().StringVar(&mcpTransport, "mcp-transport", "", "set mcp transport to stdio or http")
	cmd.Flags().StringVar(&mcpListenAddr, "mcp-listen-addr", "", "set the reserved mcp listen address")
	cmd.Flags().StringVar(&downloadsDir, "downloads-dir", "", "set the inbox download directory")
	cmd.Flags().Int64Var(&maxAttachmentSize, "max-attachment-bytes", 0, "set the max attachment size")
	cmd.Flags().StringVar(&presence, "presence", "", "set presence to online, idle, dnd, or invisible")
	cmd.Flags().StringVar(&activityType, "activity-type", "", "set activity type")
	cmd.Flags().StringVar(&activityText, "activity-text", "", "set activity text")

	return cmd
}

// --- Helpers ---

func readTokenFromReader(r io.Reader) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("failed to read bot token from stdin: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("bot token from stdin must not be empty")
	}
	return token, nil
}

func compactValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

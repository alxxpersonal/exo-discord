package cli

import (
	"context"
	"errors"
	"os/signal"
	"syscall"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/channelbridge"
	"github.com/alxxpersonal/exo-discord/internal/hook"
	runtimepkg "github.com/alxxpersonal/exo-discord/internal/runtime"
	"github.com/spf13/cobra"
)

// --- Codex Bridge Command ---

func newCodexBridgeCommand(env Environment) *cobra.Command {
	var (
		transport       string
		socketPath      string
		websocketURL    string
		threadID        string
		mirrorResponses bool
	)

	cmd := &cobra.Command{
		Use:   "codex-bridge",
		Short: "Bridge inbound Discord messages into a running Codex app-server",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}

			if err := validateCodexBridgeFlags(transport, socketPath, websocketURL); err != nil {
				return err
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}

			adapter, err := env.NewChannelAdapter(channelbridge.Config{
				Enabled: []string{"codex"},
				Codex: channelbridge.CodexConfig{
					Transport:       transport,
					SocketPath:      socketPath,
					WebsocketURL:    websocketURL,
					ThreadID:        threadID,
					MirrorResponses: mirrorResponses,
				},
			}, channelbridge.HookEnv{
				HomeDir: env.HomeDir,
				Session: session,
			})
			if err != nil {
				return err
			}
			defer func() {
				_ = adapter.Close()
			}()

			ctx, stop := signal.NotifyContext(env.commandContext(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			accessManager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
			if err != nil {
				return err
			}

			service := runtimepkg.New(
				session,
				accessManager,
				&channelDispatchHook{
					dispatch: func(ctx context.Context, envelope hook.Envelope) error {
						return adapter.Deliver(ctx, eventFromEnvelope(envelope))
					},
				},
				audit.NewLogger(resolved.AuditLogPath),
				newLogger(resolved.Config.Logging, cmd.ErrOrStderr()),
				resolved.Config.Hook.Timeout.Duration(),
				statusRequestFromConfig(resolved.Config.Status),
			)

			return ignoreContextError(service.Run(ctx))
		},
	}

	cmd.Flags().StringVar(&transport, "transport", codexTransportUnix, "codex transport: unix or ws")
	cmd.Flags().StringVar(&socketPath, "socket", "", "path to the codex unix socket")
	cmd.Flags().StringVar(&websocketURL, "websocket-url", "", "websocket url for codex app-server")
	cmd.Flags().StringVar(&threadID, "thread", "", "explicit codex thread id")
	cmd.Flags().BoolVar(&mirrorResponses, "mirror-responses", false, "reply back to Discord when Codex completes a turn")
	return cmd
}

// --- Helpers ---

const (
	codexTransportUnix = "unix"
	codexTransportWS   = "ws"
)

func validateCodexBridgeFlags(transport string, socketPath string, websocketURL string) error {
	switch transport {
	case codexTransportUnix:
		if socketPath == "" {
			return errors.New("codex unix transport requires --socket")
		}
	case codexTransportWS:
		if websocketURL == "" {
			return errors.New("codex websocket transport requires --websocket-url")
		}
	default:
		return errors.New("codex transport must be one of: unix, ws")
	}
	return nil
}

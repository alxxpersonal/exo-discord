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
	mcppkg "github.com/alxxpersonal/exo-discord/internal/mcp"
	runtimepkg "github.com/alxxpersonal/exo-discord/internal/runtime"
	"github.com/spf13/cobra"
)

// --- Channel Plugin Command ---

func newChannelPluginCommand(env Environment) *cobra.Command {
	return &cobra.Command{
		Use:   "channel-plugin",
		Short: "Run the Claude Code channel plugin bridge over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := env.resolveConfig()
			if err != nil {
				return err
			}
			if !resolved.Config.MCPEnabled {
				return errors.New("mcp is disabled")
			}

			session, err := env.NewSession(resolved)
			if err != nil {
				return err
			}
			manager, err := env.NewManager(resolved)
			if err != nil {
				return err
			}

			ctx, stop := signal.NotifyContext(env.commandContext(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			if err := manager.Open(ctx); err != nil {
				return err
			}
			defer func() {
				_ = manager.Close(context.Background())
				closeWriter(cmd.OutOrStdout())
			}()

			server := mcppkg.NewServerWithOptions(session, manager, mcppkg.Options{
				Input:  cmd.InOrStdin(),
				Output: cmd.OutOrStdout(),
				Channel: mcppkg.ChannelOptions{
					Enabled:         true,
					PermissionRelay: resolved.Config.Channel.Claude.PermissionRelay,
					AuditWriter:     channelbridge.NewAuditWriter(env.HomeDir),
				},
			})

			serverErr := make(chan error, 1)
			go func() {
				serverErr <- server.Run(ctx)
			}()

			if err := server.WaitUntilReady(ctx); err != nil {
				stop()
				return ignoreContextError(err)
			}

			accessManager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
			if err != nil {
				stop()
				return err
			}

			service := runtimepkg.New(
				session,
				accessManager,
				&channelDispatchHook{
					dispatch: func(_ context.Context, envelope hook.Envelope) error {
						event := eventFromEnvelope(envelope)
						content, meta := channelbridge.BuildClaudeNotificationPayload(event)
						return server.SendChannelNotification(content, meta)
					},
				},
				audit.NewLogger(resolved.AuditLogPath),
				newLogger(resolved.Config.Logging, cmd.ErrOrStderr()),
				resolved.Config.Hook.Timeout.Duration(),
				statusRequestFromConfig(resolved.Config.Status),
			)

			serviceErr := make(chan error, 1)
			go func() {
				serviceErr <- service.Run(ctx)
			}()

			select {
			case err := <-serviceErr:
				stop()
				<-serverErr
				return ignoreContextError(err)
			case err := <-serverErr:
				stop()
				<-serviceErr
				return ignoreContextError(err)
			case <-ctx.Done():
				stop()
				<-serviceErr
				<-serverErr
				return nil
			}
		},
	}
}

// --- Hook Bridge ---

type channelDispatchHook struct {
	dispatch func(context.Context, hook.Envelope) error
}

func (h *channelDispatchHook) Decide(ctx context.Context, envelope hook.Envelope) (hook.Response, error) {
	if h.dispatch == nil {
		return hook.Response{Decision: hook.DecisionSkip}, nil
	}
	if err := h.dispatch(ctx, envelope); err != nil {
		return hook.Response{}, err
	}
	return hook.Response{Decision: hook.DecisionSkip}, nil
}

// --- Helpers ---

func eventFromEnvelope(envelope hook.Envelope) channelbridge.Event {
	attachments := make([]channelbridge.AttachmentEvent, 0, len(envelope.Message.Attachments))
	for _, attachment := range envelope.Message.Attachments {
		attachments = append(attachments, channelbridge.AttachmentEvent{
			ID:          attachment.ID,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
			SizeBytes:   attachment.SizeBytes,
		})
	}

	return channelbridge.Event{
		ID:         envelope.ID,
		Source:     envelope.Source,
		Mode:       envelope.Mode,
		ReceivedAt: envelope.ReceivedAt,
		Message: channelbridge.MessageEvent{
			ID:             envelope.Message.ID,
			ChannelID:      envelope.Message.ChannelID,
			ChannelType:    envelope.Message.ChannelType,
			GuildID:        envelope.Message.GuildID,
			ThreadParentID: envelope.Message.ThreadParentID,
			AuthorID:       envelope.Message.AuthorID,
			AuthorUsername: envelope.Message.AuthorUsername,
			RoleIDs:        append([]string(nil), envelope.Message.RoleIDs...),
			Content:        envelope.Message.Content,
			MentionedBot:   envelope.Message.MentionedBot,
			RepliedToBot:   envelope.Message.RepliedToBot,
			Attachments:    attachments,
			Timestamp:      envelope.Message.Timestamp,
		},
		Access: channelbridge.AccessEvent{
			Reason:             envelope.Access.Reason,
			EffectiveChannelID: envelope.Access.EffectiveChannelID,
		},
	}
}

func ignoreContextError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func closeWriter(writer any) {
	if closer, ok := writer.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

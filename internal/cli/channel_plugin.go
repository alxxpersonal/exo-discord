package cli

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/channelbridge"
	"github.com/alxxpersonal/exo-discord/internal/hook"
	mcppkg "github.com/alxxpersonal/exo-discord/internal/mcp"
	runtimepkg "github.com/alxxpersonal/exo-discord/internal/runtime"
	"github.com/spf13/cobra"
)

// --- Channel Plugin Command ---

const channelPluginDrainTimeout = 2 * time.Second

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

			baseCtx := env.commandContext()
			signalCtx, stopSignal := signal.NotifyContext(baseCtx, syscall.SIGINT, syscall.SIGTERM)
			defer stopSignal()

			serverCtx, stopServer := context.WithCancel(baseCtx)
			defer stopServer()
			serviceCtx, stopService := context.WithCancel(baseCtx)
			defer stopService()

			if err := manager.Open(serviceCtx); err != nil {
				return err
			}
			defer func() {
				_ = manager.Close(context.Background())
				closeWriter(cmd.OutOrStdout())
			}()

			logger := newLogger(resolved.Config.Logging, cmd.ErrOrStderr())
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
				serverErr <- server.Run(serverCtx)
			}()

			if err := server.WaitUntilReady(serverCtx); err != nil {
				stopService()
				stopServer()
				return ignoreContextError(err)
			}

			// access evaluator, distinct from discord.Manager above for MCP tool calls.
			accessManager, err := access.NewManager(resolved.AccessStatePath, accessPolicyFromConfig(resolved.Config))
			if err != nil {
				stopService()
				stopServer()
				return err
			}

			auditor := audit.NewLogger(resolved.AuditLogPath)
			accessManager.SetLogger(logger)
			if err := accessManager.StartAutoReload(serviceCtx, resolved.ConfigPath, auditor); err != nil {
				stopService()
				stopServer()
				return err
			}

			dispatchHook := &channelDispatchHook{
				dispatch: func(_ context.Context, envelope hook.Envelope) error {
					event := eventFromEnvelope(envelope)
					content, meta := channelbridge.BuildClaudeNotificationPayload(event)
					return server.SendChannelNotification(content, meta)
				},
			}

			service := runtimepkg.New(
				session,
				accessManager,
				dispatchHook,
				auditor,
				logger,
				resolved.Config.Hook.Timeout.Duration(),
				statusRequestFromConfig(resolved.Config.Status),
			)

			serviceErr := make(chan error, 1)
			go func() {
				serviceErr <- service.Run(serviceCtx)
			}()

			var shutdownOnce sync.Once
			shutdown := func() {
				shutdownOnce.Do(func() {
					stopService()
					drainChannelDispatchHook(logger, dispatchHook)
					stopServer()
				})
			}

			select {
			case err := <-serviceErr:
				shutdown()
				<-serverErr
				return ignoreContextError(err)
			case err := <-serverErr:
				shutdown()
				<-serviceErr
				return ignoreContextError(err)
			case <-signalCtx.Done():
				shutdown()
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

	mu       sync.Mutex
	wg       sync.WaitGroup
	inflight int64
	dropped  int64
	draining bool
}

func (h *channelDispatchHook) Decide(ctx context.Context, envelope hook.Envelope) (hook.Response, error) {
	if h.dispatch == nil {
		return hook.Response{Decision: hook.DecisionSkip}, nil
	}
	if !h.start() {
		return hook.Response{Decision: hook.DecisionSkip}, nil
	}
	defer h.finish()
	if err := h.dispatch(ctx, envelope); err != nil {
		return hook.Response{}, err
	}
	return hook.Response{Decision: hook.DecisionSkip}, nil
}

func (h *channelDispatchHook) start() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.draining {
		h.dropped++
		return false
	}

	h.inflight++
	h.wg.Add(1)
	return true
}

func (h *channelDispatchHook) finish() {
	h.mu.Lock()
	if h.inflight > 0 {
		h.inflight--
	}
	h.mu.Unlock()
	h.wg.Done()
}

func (h *channelDispatchHook) Drain(ctx context.Context) int64 {
	h.mu.Lock()
	h.draining = true
	h.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.inflight + h.dropped
	case <-done:
		return 0
	}
}

// --- Helpers ---

func drainChannelDispatchHook(logger *slog.Logger, dispatchHook *channelDispatchHook) {
	if logger == nil || dispatchHook == nil {
		return
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), channelPluginDrainTimeout)
	defer cancel()

	if droppedEvents := dispatchHook.Drain(drainCtx); droppedEvents > 0 {
		logger.Warn("channel-plugin shutdown drain timed out", "component", "channel-plugin", "dropped_events", droppedEvents)
	}
}

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

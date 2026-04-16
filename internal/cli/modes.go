package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/alxxpersonal/exo-discord/internal/access"
	"github.com/alxxpersonal/exo-discord/internal/audit"
	"github.com/alxxpersonal/exo-discord/internal/config"
	"github.com/alxxpersonal/exo-discord/internal/discord"
	hookpkg "github.com/alxxpersonal/exo-discord/internal/hook"
	runtimepkg "github.com/alxxpersonal/exo-discord/internal/runtime"
)

// --- Runner Types ---

type defaultListenRunner struct{}

// Run starts listen mode with a stdout hook.
func (defaultListenRunner) Run(ctx context.Context, req listenRequest) error {
	manager, err := access.NewManager(req.Config.AccessStatePath, accessPolicyFromConfig(req.Config.Config))
	if err != nil {
		return err
	}

	service := runtimepkg.New(
		req.Session,
		manager,
		&stdoutHook{writer: req.Stdout},
		audit.NewLogger(req.Config.AuditLogPath),
		newLogger(req.Config.Config.Logging, req.Stderr),
		req.Config.Config.Hook.Timeout.Duration(),
		statusRequestFromConfig(req.Config.Config.Status),
	)
	return service.Run(ctx)
}

type defaultBotModeRunner struct{}

// Run starts bot mode with the configured hook client.
func (defaultBotModeRunner) Run(ctx context.Context, req botModeRequest) error {
	manager, err := access.NewManager(req.Config.AccessStatePath, accessPolicyFromConfig(req.Config.Config))
	if err != nil {
		return err
	}

	hookClient, err := newHookClient(req.Config.Config.Hook, req.Stderr)
	if err != nil {
		return err
	}

	service := runtimepkg.New(
		req.Session,
		manager,
		hookClient,
		audit.NewLogger(req.Config.AuditLogPath),
		newLogger(req.Config.Config.Logging, req.Stderr),
		req.Config.Config.Hook.Timeout.Duration(),
		statusRequestFromConfig(req.Config.Config.Status),
	)
	return service.Run(ctx)
}

// --- Hook Types ---

type stdoutHook struct {
	mu     sync.Mutex
	writer io.Writer
}

// Decide writes an inbound envelope to stdout.
func (h *stdoutHook) Decide(_ context.Context, envelope hookpkg.Envelope) (hookpkg.Response, error) {
	data, err := json.Marshal(envelope)
	if err != nil {
		return hookpkg.Response{}, fmt.Errorf("failed to encode listen envelope: %w", err)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.writer.Write(append(data, '\n')); err != nil {
		return hookpkg.Response{}, fmt.Errorf("failed to write listen envelope: %w", err)
	}
	return hookpkg.Response{Decision: hookpkg.DecisionSkip}, nil
}

// --- Hook Helpers ---

func newHookClient(cfg config.HookConfig, stderr io.Writer) (hookpkg.Client, error) {
	switch cfg.Kind {
	case config.HookKindNone:
		return nil, nil
	case config.HookKindHTTP:
		return hookpkg.NewHTTPClient(cfg.HTTP.URL, cfg.Timeout.Duration(), cfg.HTTP.Headers)
	case config.HookKindStdio:
		return hookpkg.NewStdioClient(cfg.Stdio.Command, stderr)
	default:
		return nil, fmt.Errorf("unsupported hook kind %q", cfg.Kind)
	}
}

// --- Logging Helpers ---

func newLogger(cfg config.LoggingConfig, stderr io.Writer) *slog.Logger {
	if stderr == nil {
		stderr = io.Discard
	}

	level := slog.LevelInfo
	switch cfg.Level {
	case config.LogLevelDebug:
		level = slog.LevelDebug
	case config.LogLevelInfo:
		level = slog.LevelInfo
	case config.LogLevelWarn:
		level = slog.LevelWarn
	case config.LogLevelError:
		level = slog.LevelError
	}

	options := &slog.HandlerOptions{Level: level}
	if cfg.Format == config.LogFormatJSON {
		return slog.New(slog.NewJSONHandler(stderr, options))
	}
	return slog.New(slog.NewTextHandler(stderr, options))
}

// --- Status Helpers ---

func statusRequestFromConfig(status config.StatusConfig) discord.StatusRequest {
	return discord.StatusRequest{
		Presence:     string(status.Presence),
		ActivityType: string(status.ActivityType),
		ActivityText: status.ActivityText,
	}
}

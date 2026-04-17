package mcp

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/alxxpersonal/exo-discord/internal/buildinfo"
	"github.com/alxxpersonal/exo-discord/internal/channelbridge"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Types ---

// Options stores MCP server runtime options.
type Options struct {
	Input   io.Reader
	Output  io.Writer
	Channel ChannelOptions
}

// ChannelOptions stores Claude channel capability options.
type ChannelOptions struct {
	Enabled         bool
	PermissionRelay bool
	AuditWriter     *channelbridge.AuditWriter
}

// Server serves the exo-discord MCP surface.
type Server struct {
	server       *sdkmcp.Server
	input        io.ReadCloser
	output       *lockedWriteCloser
	notification *channelbridge.ClaudeAdapter

	readyOnce sync.Once
	readyCh   chan struct{}
}

type lockedWriteCloser struct {
	mu sync.Mutex
	w  io.WriteCloser
}

type nopWriteCloser struct {
	io.Writer
}

// --- Constructors ---

// NewServer creates an MCP server for a Discord session.
func NewServer(session discordpkg.Session, manager discordpkg.Manager) *Server {
	return NewServerWithOptions(session, manager, Options{})
}

// NewServerWithOptions creates an MCP server with explicit runtime options.
func NewServerWithOptions(session discordpkg.Session, manager discordpkg.Manager, options Options) *Server {
	input := wrapReadCloser(orReader(options.Input, os.Stdin))
	output := &lockedWriteCloser{w: wrapWriteCloser(orWriter(options.Output, os.Stdout))}

	srv := &Server{
		input:   input,
		output:  output,
		readyCh: make(chan struct{}),
	}

	srv.server = sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "exo-discord",
		Version: buildinfo.Version,
	}, &sdkmcp.ServerOptions{
		Capabilities: buildCapabilities(options.Channel),
		InitializedHandler: func(context.Context, *sdkmcp.InitializedRequest) {
			srv.readyOnce.Do(func() {
				close(srv.readyCh)
			})
		},
	})

	registerTools(srv.server, session, manager)

	if options.Channel.Enabled {
		srv.notification = channelbridge.NewClaudeAdapter(output, options.Channel.AuditWriter)
	}

	return srv
}

// --- Runtime ---

// Run serves the MCP server over the configured transport.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &sdkmcp.IOTransport{
		Reader: s.input,
		Writer: s.output,
	})
}

// WaitUntilReady waits for the MCP session to finish initialization.
func (s *Server) WaitUntilReady(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.readyCh:
		return nil
	}
}

// SendChannelNotification emits a Claude channel notification over stdio.
func (s *Server) SendChannelNotification(content string, meta map[string]any) error {
	if s.notification == nil {
		return fmt.Errorf("channel notifications are not enabled")
	}
	return s.notification.SendChannelNotification(content, meta)
}

// Raw returns the underlying MCP server.
func (s *Server) Raw() *sdkmcp.Server {
	return s.server
}

// --- Helpers ---

func buildCapabilities(channel ChannelOptions) *sdkmcp.ServerCapabilities {
	capabilities := &sdkmcp.ServerCapabilities{
		Logging: &sdkmcp.LoggingCapabilities{},
	}

	if channel.Enabled {
		capabilities.Experimental = map[string]any{
			"claude/channel": map[string]any{},
		}
		if channel.PermissionRelay {
			capabilities.Experimental["claude/channel/permission"] = map[string]any{}
		}
	}

	return capabilities
}

func orReader(value io.Reader, fallback *os.File) io.Reader {
	if value != nil {
		return value
	}
	return fallback
}

func orWriter(value io.Writer, fallback *os.File) io.Writer {
	if value != nil {
		return value
	}
	return fallback
}

func wrapReadCloser(reader io.Reader) io.ReadCloser {
	if readCloser, ok := reader.(io.ReadCloser); ok {
		return readCloser
	}
	return io.NopCloser(reader)
}

func wrapWriteCloser(writer io.Writer) io.WriteCloser {
	if writeCloser, ok := writer.(io.WriteCloser); ok {
		return writeCloser
	}
	return nopWriteCloser{Writer: writer}
}

func (w *lockedWriteCloser) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}

func (w *lockedWriteCloser) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Close()
}

func (nopWriteCloser) Close() error {
	return nil
}

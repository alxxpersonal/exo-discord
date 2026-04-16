package mcp

import (
	"context"

	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Types ---

// Server serves the exo-discord MCP surface.
type Server struct {
	server *sdkmcp.Server
}

// --- Constructors ---

// NewServer creates an MCP server for a Discord session.
func NewServer(session discordpkg.Session) *Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    "exo-discord",
		Version: "0.1.0",
	}, nil)

	registerTools(server, session)

	return &Server{server: server}
}

// --- Runtime ---

// Run serves the MCP server over stdio.
func (s *Server) Run(ctx context.Context) error {
	return s.server.Run(ctx, &sdkmcp.StdioTransport{})
}

// Raw returns the underlying MCP server.
func (s *Server) Raw() *sdkmcp.Server {
	return s.server
}

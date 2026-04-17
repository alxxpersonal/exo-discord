package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alxxpersonal/exo-discord/internal/config"
	discordpkg "github.com/alxxpersonal/exo-discord/internal/discord"
	botpkg "github.com/alxxpersonal/exo-discord/internal/discord/bot"
	mcppkg "github.com/alxxpersonal/exo-discord/internal/mcp"
	"github.com/bwmarrin/discordgo"
	"github.com/spf13/cobra"
)

// --- Types ---

// Environment holds CLI dependencies and io handles.
type Environment struct {
	StartDir       string
	HomeDir        string
	Stdin          io.Reader
	Stdout         io.Writer
	Stderr         io.Writer
	Context        func() context.Context
	DiscoverConfig func(string, string) (config.ResolvedConfig, error)
	NewSession     func(config.ResolvedConfig) (discordpkg.Session, error)
	NewMCPServer   func(discordpkg.Session) mcpServer
	ListenRunner   listenRunner
	BotModeRunner  botModeRunner
	OAuthOverride  oauthEndpointOverride
	OAuthAPIBase   string
}

// oauthEndpointOverride lets tests redirect Discord oauth endpoints to a local server.
type oauthEndpointOverride struct {
	AuthorizeBase string
	TokenBase     string
	RevokeBase    string
}

// --- Internal Types ---

type mcpServer interface {
	Run(context.Context) error
}

type listenRequest struct {
	Config  config.ResolvedConfig
	Session discordpkg.Session
	Stdout  io.Writer
	Stderr  io.Writer
}

type listenRunner interface {
	Run(context.Context, listenRequest) error
}

type botModeRequest struct {
	Config  config.ResolvedConfig
	Session discordpkg.Session
	Stdout  io.Writer
	Stderr  io.Writer
}

type botModeRunner interface {
	Run(context.Context, botModeRequest) error
}

// --- Constructors ---

// DefaultEnvironment returns the default CLI environment.
func DefaultEnvironment() (Environment, error) {
	startDir, err := os.Getwd()
	if err != nil {
		return Environment{}, fmt.Errorf("failed to resolve cwd: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Environment{}, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	return Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdin:    os.Stdin,
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
	}, nil
}

func (env Environment) withDefaults() Environment {
	if env.Stdin == nil {
		env.Stdin = os.Stdin
	}
	if env.Stdout == nil {
		env.Stdout = os.Stdout
	}
	if env.Stderr == nil {
		env.Stderr = os.Stderr
	}
	if env.Context == nil {
		env.Context = context.Background
	}
	if env.DiscoverConfig == nil {
		env.DiscoverConfig = config.DiscoverFrom
	}
	if env.NewSession == nil {
		env.NewSession = defaultSessionFactory
	}
	if env.NewMCPServer == nil {
		env.NewMCPServer = func(session discordpkg.Session) mcpServer {
			return mcppkg.NewServer(session)
		}
	}
	if env.ListenRunner == nil {
		env.ListenRunner = defaultListenRunner{}
	}
	if env.BotModeRunner == nil {
		env.BotModeRunner = defaultBotModeRunner{}
	}
	return env
}

// --- Root Commands ---

// NewRootCommand builds the root exo-discord command tree.
func NewRootCommand(env Environment) *cobra.Command {
	env = env.withDefaults()

	root := &cobra.Command{
		Use:           "exo-discord",
		Short:         "Discord bot runtime, MCP server, and CLI for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetIn(env.Stdin)
	root.SetOut(env.Stdout)
	root.SetErr(env.Stderr)

	root.AddCommand(newInitCommand(env))
	root.AddCommand(newConfigureCommand(env))
	root.AddCommand(newDoctorCommand(env))
	root.AddCommand(newSendCommand(env))
	root.AddCommand(newListenCommand(env))
	root.AddCommand(newBotModeCommand(env))
	root.AddCommand(newAccessCommand(env))
	root.AddCommand(newPairCommand(env))
	root.AddCommand(newMCPCommand(env))
	root.AddCommand(newUserInstallModeCommand())
	root.AddCommand(newAuthCommand(env))

	return root
}

// Execute runs the root exo-discord command.
func Execute() error {
	env, err := DefaultEnvironment()
	if err != nil {
		return err
	}

	return NewRootCommand(env).Execute()
}

// --- Environment Helpers ---

func (env Environment) resolveConfig() (config.ResolvedConfig, error) {
	return env.DiscoverConfig(env.StartDir, env.HomeDir)
}

func (env Environment) commandContext() context.Context {
	return env.Context()
}

// --- Session Helpers ---

func defaultSessionFactory(resolved config.ResolvedConfig) (discordpkg.Session, error) {
	if resolved.Config.Mode == config.ModeUserInstall {
		return buildUserInstallSession(resolved)
	}

	token := strings.TrimSpace(resolved.Config.BotToken)
	if token == "" {
		return nil, errors.New("bot token is required")
	}

	client, err := botpkg.NewDiscordGoClient(token, defaultGatewayIntents)
	if err != nil {
		return nil, err
	}

	return botpkg.NewSession(client), nil
}

// --- Gateway Intents ---

const defaultGatewayIntents = discordgo.IntentGuilds |
	discordgo.IntentGuildMessages |
	discordgo.IntentDirectMessages |
	discordgo.IntentGuildMessageReactions |
	discordgo.IntentDirectMessageReactions |
	discordgo.IntentMessageContent

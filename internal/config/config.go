package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// --- Path Names ---

const (
	projectConfigName = ".exo-discord"
	homeDirName       = ".exo-discord"
	homeConfigName    = "config.toml"
	accessStateName   = "access.json"
	auditLogName      = "audit.log"
	inboxDirName      = "inbox"
	oauthDirName      = "oauth"
)

// --- Path Helpers ---

// ProjectConfigPath returns the project config path for a start directory.
func ProjectConfigPath(startDir string) string {
	return filepath.Join(startDir, projectConfigName)
}

// HomeStateDir returns the home state directory path.
func HomeStateDir(homeDir string) string {
	return filepath.Join(homeDir, homeDirName)
}

// HomeConfigPath returns the home config file path.
func HomeConfigPath(homeDir string) string {
	return filepath.Join(HomeStateDir(homeDir), homeConfigName)
}

// AccessStatePath returns the access state file path.
func AccessStatePath(homeDir string) string {
	return filepath.Join(HomeStateDir(homeDir), accessStateName)
}

// HomeDirFromAccessStatePath returns the home directory for an access state path.
func HomeDirFromAccessStatePath(statePath string) string {
	return filepath.Dir(filepath.Dir(filepath.Clean(statePath)))
}

// AuditLogPath returns the audit log path.
func AuditLogPath(homeDir string) string {
	return filepath.Join(HomeStateDir(homeDir), auditLogName)
}

// InboxDirPath returns the inbox directory path.
func InboxDirPath(homeDir string) string {
	return filepath.Join(HomeStateDir(homeDir), inboxDirName)
}

// OAuthDirPath returns the oauth token directory path.
func OAuthDirPath(homeDir string) string {
	return filepath.Join(HomeStateDir(homeDir), oauthDirName)
}

// --- Mode Types ---

// Mode names the configured runtime mode.
type Mode string

const (
	ModeBot         Mode = "bot"
	ModeUserInstall Mode = "user_install"
)

// --- Hook Types ---

// HookKind names the configured hook transport.
type HookKind string

const (
	HookKindNone  HookKind = "none"
	HookKindHTTP  HookKind = "http"
	HookKindStdio HookKind = "stdio"
)

// MCPTransport names the configured MCP transport.
type MCPTransport string

const (
	MCPTransportStdio MCPTransport = "stdio"
	MCPTransportHTTP  MCPTransport = "http"
)

// --- Logging Types ---

// LogLevel names the configured log verbosity.
type LogLevel string

const (
	LogLevelDebug LogLevel = "debug"
	LogLevelInfo  LogLevel = "info"
	LogLevelWarn  LogLevel = "warn"
	LogLevelError LogLevel = "error"
)

// LogFormat names the configured log output format.
type LogFormat string

const (
	LogFormatText LogFormat = "text"
	LogFormatJSON LogFormat = "json"
)

// --- Status Types ---

// Presence names the configured Discord presence.
type Presence string

const (
	PresenceOnline    Presence = "online"
	PresenceIdle      Presence = "idle"
	PresenceDND       Presence = "dnd"
	PresenceInvisible Presence = "invisible"
)

// ActivityType names the configured Discord activity type.
type ActivityType string

const (
	ActivityTypeNone      ActivityType = ""
	ActivityTypePlaying   ActivityType = "playing"
	ActivityTypeStreaming ActivityType = "streaming"
	ActivityTypeListening ActivityType = "listening"
	ActivityTypeWatching  ActivityType = "watching"
	ActivityTypeCompeting ActivityType = "competing"
)

// --- Duration Helpers ---

// Duration stores a TOML duration value.
type Duration time.Duration

// Duration returns the native time.Duration value.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// String returns the duration text form.
func (d Duration) String() string {
	return time.Duration(d).String()
}

// MarshalText encodes the duration as text.
func (d Duration) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText decodes the duration from text.
func (d *Duration) UnmarshalText(text []byte) error {
	parsed, err := time.ParseDuration(string(text))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", string(text), err)
	}
	*d = Duration(parsed)
	return nil
}

// --- Config Types ---

// Config stores the exo-discord configuration.
type Config struct {
	Mode              Mode           `toml:"mode"`
	BotToken          string         `toml:"bot_token"`
	MCPEnabled        bool           `toml:"mcp_enabled"`
	CLIEnabled        bool           `toml:"cli_enabled"`
	RequireMention    bool           `toml:"require_mention"`
	AllowedUserIDs    []string       `toml:"allowed_user_ids"`
	AllowedChannelIDs []string       `toml:"allowed_channel_ids"`
	AllowedRoleIDs    []string       `toml:"allowed_role_ids"`
	Pairing           PairingConfig  `toml:"pairing"`
	Hook              HookConfig     `toml:"hook"`
	MCP               MCPConfig      `toml:"mcp"`
	Channel           ChannelConfig  `toml:"channel"`
	Downloads         DownloadConfig `toml:"downloads"`
	Logging           LoggingConfig  `toml:"logging"`
	Status            StatusConfig   `toml:"status"`
	OAuth             OAuthConfig    `toml:"oauth"`
}

// OAuthConfig stores the Discord oauth 2.0 client configuration for user-install mode.
type OAuthConfig struct {
	ClientID     string   `toml:"client_id"`
	ClientSecret string   `toml:"client_secret"`
	RedirectURI  string   `toml:"redirect_uri"`
	Scopes       []string `toml:"scopes"`
	// ForceConsent appends `prompt=consent` to the authorize URL so Discord always
	// re-asks the user for authorization. Defaults to false.
	ForceConsent bool `toml:"force_consent"`
}

// PairingConfig stores DM pairing settings.
type PairingConfig struct {
	Enabled     bool     `toml:"enabled"`
	CodeTTL     Duration `toml:"code_ttl"`
	MaxPending  int      `toml:"max_pending"`
	ResendLimit int      `toml:"resend_limit"`
}

// HookConfig stores hook settings.
type HookConfig struct {
	Kind    HookKind        `toml:"kind"`
	Timeout Duration        `toml:"timeout"`
	HTTP    HookHTTPConfig  `toml:"http"`
	Stdio   HookStdioConfig `toml:"stdio"`
}

// HookHTTPConfig stores HTTP hook settings.
type HookHTTPConfig struct {
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
}

// HookStdioConfig stores stdio hook settings.
type HookStdioConfig struct {
	Command []string `toml:"command"`
}

// MCPConfig stores MCP settings.
type MCPConfig struct {
	Transport  MCPTransport `toml:"transport"`
	ListenAddr string       `toml:"listen_addr"`
}

// ChannelConfig stores channel bridge settings.
type ChannelConfig struct {
	Enabled []string            `toml:"enabled"`
	Claude  ChannelClaudeConfig `toml:"claude"`
	Codex   ChannelCodexConfig  `toml:"codex"`
}

// ChannelClaudeConfig stores Claude bridge settings.
type ChannelClaudeConfig struct {
	PermissionRelay bool `toml:"permission_relay"`
}

// ChannelCodexConfig stores Codex bridge settings.
type ChannelCodexConfig struct {
	Transport          string   `toml:"transport"`
	SocketPath         string   `toml:"socket_path"`
	WebsocketURL       string   `toml:"websocket_url"`
	ThreadID           string   `toml:"thread_id"`
	MirrorResponses    bool     `toml:"mirror_responses"`
	MirrorFlushTimeout Duration `toml:"mirror_flush_timeout"`
	AutoCreateThread   bool     `toml:"auto_create_thread"`
}

// DownloadConfig stores attachment download settings.
type DownloadConfig struct {
	Dir                string `toml:"dir"`
	MaxAttachmentBytes int64  `toml:"max_attachment_bytes"`
}

// LoggingConfig stores logger settings.
type LoggingConfig struct {
	Level  LogLevel  `toml:"level"`
	Format LogFormat `toml:"format"`
}

// StatusConfig stores Discord status settings.
type StatusConfig struct {
	Presence     Presence     `toml:"presence"`
	ActivityType ActivityType `toml:"activity_type"`
	ActivityText string       `toml:"activity_text"`
}

// ResolvedConfig stores discovered config paths and values.
type ResolvedConfig struct {
	Config          Config
	ConfigPath      string
	HomeStateDir    string
	HomeConfigPath  string
	AccessStatePath string
	AuditLogPath    string
	InboxDirPath    string
	OAuthDirPath    string
}

// --- Defaults ---

func defaultConfig(homeDir string) Config {
	stateDir := filepath.Join(homeDir, homeDirName)
	return Config{
		Mode:           ModeBot,
		MCPEnabled:     true,
		CLIEnabled:     true,
		RequireMention: true,
		Pairing: PairingConfig{
			Enabled:     true,
			CodeTTL:     Duration(time.Hour),
			MaxPending:  32,
			ResendLimit: 2,
		},
		Hook: HookConfig{
			Kind:    HookKindNone,
			Timeout: Duration(15 * time.Second),
			HTTP: HookHTTPConfig{
				Headers: map[string]string{},
			},
		},
		MCP: MCPConfig{
			Transport: MCPTransportStdio,
		},
		Channel: ChannelConfig{
			Claude: ChannelClaudeConfig{
				PermissionRelay: false,
			},
			Codex: ChannelCodexConfig{
				Transport:          "unix",
				SocketPath:         filepath.Join(homeDir, ".codex", "sessions", "default", "broker.sock"),
				MirrorResponses:    true,
				MirrorFlushTimeout: Duration(30 * time.Second),
				AutoCreateThread:   true,
			},
		},
		Downloads: DownloadConfig{
			Dir:                filepath.Join(stateDir, inboxDirName),
			MaxAttachmentBytes: 25 * 1024 * 1024,
		},
		Logging: LoggingConfig{
			Level:  LogLevelInfo,
			Format: LogFormatText,
		},
		Status: StatusConfig{
			Presence: PresenceOnline,
		},
		OAuth: OAuthConfig{
			Scopes: []string{"identify", "guilds"},
		},
	}
}

// --- Normalization Helpers ---

func (c *Config) normalize(homeDir string) {
	c.AllowedUserIDs = dedupe(c.AllowedUserIDs)
	c.AllowedChannelIDs = dedupe(c.AllowedChannelIDs)
	c.AllowedRoleIDs = dedupe(c.AllowedRoleIDs)
	c.Channel.Enabled = dedupe(c.Channel.Enabled)

	c.Channel.Codex.Transport = strings.TrimSpace(c.Channel.Codex.Transport)
	c.Channel.Codex.SocketPath = expandHome(strings.TrimSpace(c.Channel.Codex.SocketPath), homeDir)
	c.Channel.Codex.WebsocketURL = strings.TrimSpace(c.Channel.Codex.WebsocketURL)
	c.Channel.Codex.ThreadID = strings.TrimSpace(c.Channel.Codex.ThreadID)
	c.Downloads.Dir = expandHome(c.Downloads.Dir, homeDir)
}

func dedupe(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func expandHome(path string, homeDir string) string {
	if path == "" {
		return ""
	}
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// --- Validation ---

// Validate checks that the config uses supported values.
func (c Config) Validate() error {
	switch c.Mode {
	case ModeBot, ModeUserInstall:
	default:
		return fmt.Errorf("unsupported mode %q", c.Mode)
	}

	switch c.Hook.Kind {
	case HookKindNone, HookKindHTTP, HookKindStdio:
	default:
		return fmt.Errorf("unsupported hook kind %q", c.Hook.Kind)
	}

	switch c.MCP.Transport {
	case MCPTransportStdio, MCPTransportHTTP:
	default:
		return fmt.Errorf("unsupported mcp transport %q", c.MCP.Transport)
	}

	switch c.Logging.Level {
	case LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError:
	default:
		return fmt.Errorf("unsupported log level %q", c.Logging.Level)
	}

	switch c.Logging.Format {
	case LogFormatText, LogFormatJSON:
	default:
		return fmt.Errorf("unsupported log format %q", c.Logging.Format)
	}

	for _, target := range c.Channel.Enabled {
		switch target {
		case "claude", "codex":
		default:
			return fmt.Errorf("unsupported channel target %q", target)
		}
	}

	switch c.Channel.Codex.Transport {
	case "", "unix", "ws":
	default:
		return fmt.Errorf("unsupported channel codex transport %q", c.Channel.Codex.Transport)
	}

	switch c.Status.Presence {
	case PresenceOnline, PresenceIdle, PresenceDND, PresenceInvisible:
	default:
		return fmt.Errorf("unsupported presence %q", c.Status.Presence)
	}

	switch c.Status.ActivityType {
	case ActivityTypeNone, ActivityTypePlaying, ActivityTypeStreaming, ActivityTypeListening, ActivityTypeWatching, ActivityTypeCompeting:
	default:
		return fmt.Errorf("unsupported activity type %q", c.Status.ActivityType)
	}

	if c.Pairing.CodeTTL.Duration() <= 0 {
		return fmt.Errorf("pairing code_ttl must be greater than zero")
	}
	if c.Pairing.MaxPending < 1 {
		return fmt.Errorf("pairing max_pending must be at least 1")
	}
	if c.Pairing.ResendLimit < 0 {
		return fmt.Errorf("pairing resend_limit must be at least 0")
	}
	if c.Hook.Timeout.Duration() <= 0 {
		return fmt.Errorf("hook timeout must be greater than zero")
	}
	if c.Downloads.MaxAttachmentBytes < 1 {
		return fmt.Errorf("downloads max_attachment_bytes must be at least 1")
	}
	if c.Downloads.Dir == "" {
		return fmt.Errorf("downloads dir must not be empty")
	}
	if c.Hook.Kind == HookKindHTTP && c.Hook.HTTP.URL == "" {
		return fmt.Errorf("hook http url must not be empty")
	}
	if c.Hook.Kind == HookKindStdio && len(c.Hook.Stdio.Command) == 0 {
		return fmt.Errorf("hook stdio command must not be empty")
	}
	if c.Channel.Codex.Transport == "unix" && c.Channel.Codex.SocketPath == "" {
		return fmt.Errorf("channel codex socket_path must not be empty for unix transport")
	}
	if c.Channel.Codex.Transport == "ws" && c.Channel.Codex.WebsocketURL == "" {
		return fmt.Errorf("channel codex websocket_url must not be empty for ws transport")
	}
	if c.Channel.Codex.MirrorFlushTimeout.Duration() < 0 {
		return fmt.Errorf("channel codex mirror_flush_timeout must be greater than or equal to zero")
	}

	if c.Mode == ModeUserInstall {
		if strings.TrimSpace(c.OAuth.ClientID) == "" {
			return fmt.Errorf("oauth client_id is required for user_install mode")
		}
		if strings.TrimSpace(c.OAuth.ClientSecret) == "" {
			return fmt.Errorf("oauth client_secret is required for user_install mode")
		}
		if strings.TrimSpace(c.OAuth.RedirectURI) == "" {
			return fmt.Errorf("oauth redirect_uri is required for user_install mode")
		}
		if len(c.OAuth.Scopes) == 0 {
			return fmt.Errorf("oauth scopes must not be empty for user_install mode")
		}
	}

	return nil
}

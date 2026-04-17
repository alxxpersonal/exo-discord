package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// --- Errors ---

// ErrConfigNotFound reports that no config file was discovered.
var ErrConfigNotFound = errors.New("config not found")

// --- Discovery ---

// Discover returns the resolved config for the current environment.
func Discover() (ResolvedConfig, error) {
	startDir, err := os.Getwd()
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("failed to resolve cwd: %w", err)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("failed to resolve home directory: %w", err)
	}

	return DiscoverFrom(startDir, homeDir)
}

// DiscoverFrom returns the resolved config for explicit start and home paths.
func DiscoverFrom(startDir string, homeDir string) (ResolvedConfig, error) {
	current := filepath.Clean(startDir)
	for {
		candidate := filepath.Join(current, projectConfigName)
		info, err := os.Stat(candidate)
		switch {
		case err == nil:
			if info.Mode().IsRegular() {
				return loadResolved(candidate, homeDir)
			}
			// Non-regular match (e.g. the home state directory shares the
			// projectConfigName). Treat it as not-a-match and keep walking so
			// the home fallback can still resolve.
		case errors.Is(err, os.ErrNotExist):
		default:
			return ResolvedConfig{}, fmt.Errorf("failed to stat %s: %w", candidate, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	fallback := filepath.Join(homeDir, homeDirName, homeConfigName)
	info, err := os.Stat(fallback)
	switch {
	case err == nil:
		if !info.Mode().IsRegular() {
			return ResolvedConfig{}, fmt.Errorf("config path %s is not a file", fallback)
		}
		return loadResolved(fallback, homeDir)
	case errors.Is(err, os.ErrNotExist):
		return ResolvedConfig{}, ErrConfigNotFound
	default:
		return ResolvedConfig{}, fmt.Errorf("failed to stat %s: %w", fallback, err)
	}
}

// --- Load Helpers ---

// LoadResolvedPath returns the resolved config for an explicit config path and home dir.
func LoadResolvedPath(configPath string, homeDir string) (ResolvedConfig, error) {
	return loadResolved(configPath, homeDir)
}

func loadResolved(configPath string, homeDir string) (ResolvedConfig, error) {
	if err := RequireExactFilePerms(configPath, 0o600); err != nil {
		return ResolvedConfig{}, err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return ResolvedConfig{}, fmt.Errorf("failed to read config %s: %w", configPath, err)
	}

	cfg := defaultConfig(homeDir)
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return ResolvedConfig{}, fmt.Errorf("failed to decode config %s: %w", configPath, err)
	}

	cfg.normalize(homeDir)
	if err := cfg.Validate(); err != nil {
		return ResolvedConfig{}, fmt.Errorf("invalid config %s: %w", configPath, err)
	}

	stateDir := filepath.Join(homeDir, homeDirName)
	return ResolvedConfig{
		Config:          cfg,
		ConfigPath:      configPath,
		HomeStateDir:    stateDir,
		HomeConfigPath:  filepath.Join(stateDir, homeConfigName),
		AccessStatePath: filepath.Join(stateDir, accessStateName),
		AuditLogPath:    filepath.Join(stateDir, auditLogName),
		InboxDirPath:    cfg.Downloads.Dir,
		OAuthDirPath:    filepath.Join(stateDir, oauthDirName),
	}, nil
}

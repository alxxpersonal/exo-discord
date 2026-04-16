package config

import (
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
)

// --- Config Writers ---

// WriteHomeConfig creates the home config file.
func WriteHomeConfig(homeDir string, force bool) (string, error) {
	stateDir := HomeStateDir(homeDir)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create state dir %s: %w", stateDir, err)
	}
	//nolint:gosec
	if err := os.Chmod(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to set state dir permissions on %s: %w", stateDir, err)
	}

	path := HomeConfigPath(homeDir)
	if err := writeConfigFile(path, defaultConfig(homeDir), force); err != nil {
		return "", err
	}
	return path, nil
}

// WriteProjectConfig creates the project config file.
func WriteProjectConfig(startDir string, homeDir string, force bool) (string, error) {
	path := ProjectConfigPath(startDir)
	if err := writeConfigFile(path, defaultConfig(homeDir), force); err != nil {
		return "", err
	}
	return path, nil
}

// --- Helpers ---

func writeConfigFile(path string, cfg Config, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config %s already exists", path)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("failed to stat config %s: %w", path, err)
		}
	}

	payload, err := toml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to encode config %s: %w", path, err)
	}
	payload = append(payload, '\n')

	//nolint:gosec
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("failed to create config parent dir %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("failed to write config %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("failed to set config permissions on %s: %w", path, err)
	}

	return nil
}

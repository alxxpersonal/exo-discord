package config

import (
	"fmt"
	"os"
)

// --- Permission Helpers ---

// RequireExactFilePerms checks that a file has the expected permissions.
func RequireExactFilePerms(path string, want os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path %s is not a regular file", path)
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("path %s must have %04o permissions, got %04o", path, want, info.Mode().Perm())
	}
	return nil
}

// RequireExactDirPerms checks that a directory has the expected permissions.
func RequireExactDirPerms(path string, want os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("path %s is not a directory", path)
	}
	if info.Mode().Perm() != want {
		return fmt.Errorf("path %s must have %04o permissions, got %04o", path, want, info.Mode().Perm())
	}
	return nil
}

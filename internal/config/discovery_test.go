package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Test Cases ---

func TestDiscoverFromPrefersNearestProjectConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	start := filepath.Join(root, "workspace", "child", "grandchild")
	projectDir := filepath.Join(root, "workspace", "child")

	mkdirAll(t, home, 0o755)
	mkdirAll(t, start, 0o755)
	writeConfig(t, filepath.Join(root, "workspace", projectConfigName), "mode = \"bot\"\n", 0o600)
	writeConfig(t, filepath.Join(projectDir, projectConfigName), "mode = \"bot\"\nallowed_user_ids = [\"1\", \"1\", \"2\"]\n", 0o600)

	resolved, err := DiscoverFrom(start, home)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}

	if got, want := resolved.ConfigPath, filepath.Join(projectDir, projectConfigName); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	if got, want := strings.Join(resolved.Config.AllowedUserIDs, ","), "1,2"; got != want {
		t.Fatalf("AllowedUserIDs = %q, want %q", got, want)
	}
}

func TestDiscoverFromFallsBackHomeConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	start := filepath.Join(root, "workspace", "child")

	mkdirAll(t, home, 0o755)
	mkdirAll(t, start, 0o755)
	mkdirAll(t, filepath.Join(home, homeDirName), 0o755)
	writeConfig(t, filepath.Join(home, homeDirName, homeConfigName), "mode = \"bot\"\n[downloads]\ndir = \"~/custom-inbox\"\n", 0o600)

	resolved, err := DiscoverFrom(start, home)
	if err != nil {
		t.Fatalf("DiscoverFrom() error = %v", err)
	}

	if got, want := resolved.ConfigPath, filepath.Join(home, homeDirName, homeConfigName); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	if got, want := resolved.InboxDirPath, filepath.Join(home, "custom-inbox"); got != want {
		t.Fatalf("InboxDirPath = %q, want %q", got, want)
	}
}

func TestDiscoverFromMissingConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	start := filepath.Join(root, "workspace")

	mkdirAll(t, home, 0o755)
	mkdirAll(t, start, 0o755)

	_, err := DiscoverFrom(start, home)
	if !errors.Is(err, ErrConfigNotFound) {
		t.Fatalf("DiscoverFrom() error = %v, want ErrConfigNotFound", err)
	}
}

func TestDiscoverFromRejectsProjectConfigPerms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	start := filepath.Join(root, "workspace")

	mkdirAll(t, home, 0o755)
	mkdirAll(t, start, 0o755)
	writeConfig(t, filepath.Join(start, projectConfigName), "mode = \"bot\"\n", 0o644)

	_, err := DiscoverFrom(start, home)
	if err == nil {
		t.Fatal("DiscoverFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "0600") {
		t.Fatalf("DiscoverFrom() error = %v, want 0600 error", err)
	}
}

func TestDiscoverFromRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	start := filepath.Join(root, "workspace")

	mkdirAll(t, home, 0o755)
	mkdirAll(t, start, 0o755)
	writeConfig(t, filepath.Join(start, projectConfigName), "mode = \"invalid\"\n", 0o600)

	_, err := DiscoverFrom(start, home)
	if err == nil {
		t.Fatal("DiscoverFrom() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "unsupported mode") {
		t.Fatalf("DiscoverFrom() error = %v, want mode error", err)
	}
}

func TestRequireExactDirPerms(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	privateDir := filepath.Join(root, "state")
	mkdirAll(t, privateDir, 0o700)

	if err := RequireExactDirPerms(privateDir, 0o700); err != nil {
		t.Fatalf("RequireExactDirPerms() error = %v", err)
	}

	//nolint:gosec
	if err := os.Chmod(privateDir, 0o755); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	if err := RequireExactDirPerms(privateDir, 0o700); err == nil {
		t.Fatal("RequireExactDirPerms() error = nil, want error")
	}
}

// --- Helpers ---

func mkdirAll(t *testing.T, path string, perm os.FileMode) {
	t.Helper()

	if err := os.MkdirAll(path, perm); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
}

func writeConfig(t *testing.T, path string, contents string, perm os.FileMode) {
	t.Helper()

	//nolint:gosec
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), perm); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}

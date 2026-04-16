package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Test Cases ---

func TestInitCommandWritesHomeConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	env := Environment{
		StartDir: filepath.Join(root, "workspace"),
		HomeDir:  filepath.Join(root, "home"),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}
	//nolint:gosec
	if err := os.MkdirAll(env.StartDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	//nolint:gosec
	if err := os.MkdirAll(env.HomeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	stdout := env.Stdout.(*bytes.Buffer)
	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"init"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	if path == "" {
		t.Fatal("stdout is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("config perms = %04o, want %04o", got, want)
	}

	stateDir := filepath.Dir(path)
	stateInfo, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", stateDir, err)
	}
	if got, want := stateInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("state dir perms = %04o, want %04o", got, want)
	}
}

func TestInitCommandWritesProjectConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	env := Environment{
		StartDir: filepath.Join(root, "workspace"),
		HomeDir:  filepath.Join(root, "home"),
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}
	//nolint:gosec
	if err := os.MkdirAll(env.StartDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	//nolint:gosec
	if err := os.MkdirAll(env.HomeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	stdout := env.Stdout.(*bytes.Buffer)
	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"init", "--project"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	path := strings.TrimSpace(stdout.String())
	want := filepath.Join(env.StartDir, ".exo-discord")
	if path != want {
		t.Fatalf("stdout path = %q, want %q", path, want)
	}
}

func TestDoctorCommandJSON(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	startDir := filepath.Join(root, "workspace")
	homeDir := filepath.Join(root, "home")
	//nolint:gosec
	if err := os.MkdirAll(startDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	//nolint:gosec
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path := filepath.Join(startDir, ".exo-discord")
	if err := os.WriteFile(path, []byte("mode = \"bot\"\nbot_token = \"secret\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	env := Environment{
		StartDir: startDir,
		HomeDir:  homeDir,
		Stdout:   &bytes.Buffer{},
		Stderr:   &bytes.Buffer{},
	}
	stdout := env.Stdout.(*bytes.Buffer)

	cmd := NewRootCommand(env)
	cmd.SetArgs([]string{"doctor", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	output := stdout.String()
	if !strings.Contains(output, "\"mode\": \"bot\"") {
		t.Fatalf("doctor output = %q, want bot mode", output)
	}
	if !strings.Contains(output, "\"bot_token_present\": true") {
		t.Fatalf("doctor output = %q, want bot token present", output)
	}
}

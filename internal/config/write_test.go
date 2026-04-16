package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- Test Cases ---

func TestPathHelpers(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(os.PathSeparator), "tmp", "home")
	start := filepath.Join(string(os.PathSeparator), "tmp", "workspace")

	if got, want := ProjectConfigPath(start), filepath.Join(start, ".exo-discord"); got != want {
		t.Fatalf("ProjectConfigPath() = %q, want %q", got, want)
	}
	if got, want := HomeStateDir(home), filepath.Join(home, ".exo-discord"); got != want {
		t.Fatalf("HomeStateDir() = %q, want %q", got, want)
	}
	if got, want := HomeConfigPath(home), filepath.Join(home, ".exo-discord", "config.toml"); got != want {
		t.Fatalf("HomeConfigPath() = %q, want %q", got, want)
	}
	if got, want := AccessStatePath(home), filepath.Join(home, ".exo-discord", "access.json"); got != want {
		t.Fatalf("AccessStatePath() = %q, want %q", got, want)
	}
	if got, want := AuditLogPath(home), filepath.Join(home, ".exo-discord", "audit.log"); got != want {
		t.Fatalf("AuditLogPath() = %q, want %q", got, want)
	}
	if got, want := InboxDirPath(home), filepath.Join(home, ".exo-discord", "inbox"); got != want {
		t.Fatalf("InboxDirPath() = %q, want %q", got, want)
	}
}

func TestDurationStringAndMarshalText(t *testing.T) {
	t.Parallel()

	value := Duration(90 * 1_000_000_000)
	if got, want := value.String(), "1m30s"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
	text, err := value.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText() error = %v", err)
	}
	if got, want := string(text), "1m30s"; got != want {
		t.Fatalf("MarshalText() = %q, want %q", got, want)
	}
}

func TestWriteHomeConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	path, err := WriteHomeConfig(home, false)
	if err != nil {
		t.Fatalf("WriteHomeConfig() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Fatalf("file perms = %04o, want %04o", got, want)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat(dir) error = %v", err)
	}
	if got, want := dirInfo.Mode().Perm(), os.FileMode(0o700); got != want {
		t.Fatalf("dir perms = %04o, want %04o", got, want)
	}

	if _, err := WriteHomeConfig(home, false); err == nil {
		t.Fatal("WriteHomeConfig() error = nil, want already exists")
	}
	if _, err := WriteHomeConfig(home, true); err != nil {
		t.Fatalf("WriteHomeConfig(force) error = %v", err)
	}
}

func TestWriteProjectConfig(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	//nolint:gosec
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	path, err := WriteProjectConfig(start, home, false)
	if err != nil {
		t.Fatalf("WriteProjectConfig() error = %v", err)
	}
	if got, want := path, filepath.Join(start, ".exo-discord"); got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
}

func TestDiscoverUsesCurrentWorkingDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	start := filepath.Join(root, "workspace")
	home := filepath.Join(root, "home")
	//nolint:gosec
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	//nolint:gosec
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(start, ".exo-discord"), []byte("mode = \"bot\"\nbot_token = \"secret\"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	defer func() {
		_ = os.Chdir(previous)
	}()
	if err := os.Chdir(start); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}

	resolved, err := Discover()
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	gotInfo, err := os.Stat(resolved.ConfigPath)
	if err != nil {
		t.Fatalf("Stat(resolved) error = %v", err)
	}
	wantPath := filepath.Join(start, ".exo-discord")
	wantInfo, err := os.Stat(wantPath)
	if err != nil {
		t.Fatalf("Stat(want) error = %v", err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Fatalf("ConfigPath = %q, want same file as %q", resolved.ConfigPath, wantPath)
	}
}

func TestConfigValidateBranches(t *testing.T) {
	t.Parallel()

	valid := defaultConfig(t.TempDir())
	valid.BotToken = "secret"
	valid.Hook.Kind = HookKindNone
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	tests := []Config{
		func() Config {
			cfg := valid
			cfg.Mode = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Hook.Kind = HookKindHTTP
			cfg.Hook.HTTP.URL = ""
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Hook.Kind = HookKindStdio
			cfg.Hook.Stdio.Command = nil
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Status.ActivityType = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Pairing.CodeTTL = Duration(0)
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Hook.Kind = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.MCP.Transport = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Logging.Level = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Logging.Format = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Status.Presence = "bad"
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Pairing.ResendLimit = -1
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Downloads.MaxAttachmentBytes = 0
			return cfg
		}(),
		func() Config {
			cfg := valid
			cfg.Downloads.Dir = ""
			return cfg
		}(),
	}

	for idx, cfg := range tests {
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate(test %d) error = nil, want error", idx)
		}
	}
}

func TestExpandHomeAndDurationDuration(t *testing.T) {
	t.Parallel()

	home := filepath.Join(string(os.PathSeparator), "tmp", "home")
	if got, want := expandHome("~/downloads", home), filepath.Join(home, "downloads"); got != want {
		t.Fatalf("expandHome() = %q, want %q", got, want)
	}
	if got, want := Duration(time.Minute).Duration(), time.Minute; got != want {
		t.Fatalf("Duration() = %v, want %v", got, want)
	}
}

func TestDefaultConfigAndNormalize(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	cfg := defaultConfig(home)
	if cfg.Downloads.Dir == "" || cfg.Pairing.CodeTTL.Duration() <= 0 {
		t.Fatalf("defaultConfig() = %#v, want populated defaults", cfg)
	}

	cfg.AllowedUserIDs = []string{" user-1 ", "user-1", ""}
	cfg.AllowedChannelIDs = []string{"chan-1", "chan-1"}
	cfg.AllowedRoleIDs = []string{"role-1", "role-1"}
	cfg.Downloads.Dir = "~/downloads"
	cfg.normalize(home)
	if got, want := cfg.AllowedUserIDs, []string{"user-1"}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("AllowedUserIDs = %v, want %v", got, want)
	}
	if got, want := cfg.Downloads.Dir, filepath.Join(home, "downloads"); got != want {
		t.Fatalf("Downloads.Dir = %q, want %q", got, want)
	}
}

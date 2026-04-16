package config

import (
	"strings"
	"testing"
	"time"
)

// --- Test Cases ---

func TestDurationUnmarshalText(t *testing.T) {
	t.Parallel()

	var duration Duration
	if err := duration.UnmarshalText([]byte("90s")); err != nil {
		t.Fatalf("UnmarshalText() error = %v", err)
	}

	if got, want := duration.Duration(), 90*time.Second; got != want {
		t.Fatalf("duration = %v, want %v", got, want)
	}
}

func TestConfigValidateAcceptsDefaults(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig(t.TempDir())
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestConfigValidateRejectsInvalidHookStdio(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig(t.TempDir())
	cfg.Hook.Kind = HookKindStdio

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "hook stdio command") {
		t.Fatalf("Validate() error = %v, want stdio command error", err)
	}
}

func TestConfigValidateRejectsInvalidHookHTTP(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig(t.TempDir())
	cfg.Hook.Kind = HookKindHTTP

	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "hook http url") {
		t.Fatalf("Validate() error = %v, want http url error", err)
	}
}

func TestConfigNormalizeDedupe(t *testing.T) {
	t.Parallel()

	cfg := defaultConfig("/tmp/home")
	cfg.AllowedUserIDs = []string{" 123 ", "123", "", "456"}
	cfg.normalize("/tmp/home")

	got := strings.Join(cfg.AllowedUserIDs, ",")
	if want := "123,456"; got != want {
		t.Fatalf("AllowedUserIDs = %q, want %q", got, want)
	}
}

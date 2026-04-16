package redact

import (
	"errors"
	"fmt"
	"testing"
)

// --- Constants ---

const sampleToken = "MTg0Njk1MDgwNzA5MzI0ODAw.ABCdef.gHIjklMNOpqrstUVWXYZ123456"

// --- Test Cases ---

func TestStringRedactsStandaloneToken(t *testing.T) {
	t.Parallel()

	input := "discord token: " + sampleToken

	if got, want := String(input), "discord token: "+placeholder; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringRedactsTokenAssignments(t *testing.T) {
	t.Parallel()

	input := `bot_token = "` + sampleToken + `"`

	if got, want := String(input), `bot_token = "`+placeholder+`"`; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringRedactsAuthorizationScheme(t *testing.T) {
	t.Parallel()

	input := "Authorization: Bot " + sampleToken

	if got, want := String(input), "Authorization: "+placeholder; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestStringLeavesSafeValues(t *testing.T) {
	t.Parallel()

	input := "bot mode enabled, bot_token_present=true"

	if got := String(input); got != input {
		t.Fatalf("String() = %q, want unchanged", got)
	}
}

func TestWrapErrorRedactsAndUnwraps(t *testing.T) {
	t.Parallel()

	cause := fmt.Errorf("discord login failed with token %s", sampleToken)
	wrapped := WrapError(cause)

	if wrapped == nil {
		t.Fatal("WrapError() = nil, want wrapped error")
	}
	if got := wrapped.Error(); got != "discord login failed with token "+placeholder {
		t.Fatalf("wrapped.Error() = %q", got)
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("errors.Is() = false, want true")
	}
}

func TestWrapErrorNil(t *testing.T) {
	t.Parallel()

	if WrapError(nil) != nil {
		t.Fatal("WrapError(nil) != nil")
	}
}

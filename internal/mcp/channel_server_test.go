package mcp

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/alxxpersonal/exo-discord/internal/buildinfo"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// --- Test Cases ---

func TestServerInitializeIncludesClaudeChannelCapabilities(t *testing.T) {
	t.Parallel()

	server := NewServerWithOptions(nil, nil, Options{
		Channel: ChannelOptions{
			Enabled:         true,
			PermissionRelay: true,
		},
	})

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Raw().Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "test-client",
		Version: buildinfo.Version,
	}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() {
		_ = session.Close()
		cancel()
		<-serverDone
	}()

	result := session.InitializeResult()
	if result == nil || result.Capabilities == nil {
		t.Fatal("InitializeResult() = nil")
	}

	experimental := result.Capabilities.Experimental
	if _, ok := experimental["claude/channel"]; !ok {
		t.Fatalf("experimental capabilities = %#v, missing claude/channel", experimental)
	}
	if _, ok := experimental["claude/channel/permission"]; !ok {
		t.Fatalf("experimental capabilities = %#v, missing claude/channel/permission", experimental)
	}
}

func TestServerInitializeOmitsPermissionCapabilityWhenDisabled(t *testing.T) {
	t.Parallel()

	server := NewServerWithOptions(nil, nil, Options{
		Channel: ChannelOptions{
			Enabled:         true,
			PermissionRelay: false,
		},
	})

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Raw().Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "test-client",
		Version: buildinfo.Version,
	}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() {
		_ = session.Close()
		cancel()
		<-serverDone
	}()

	experimental := session.InitializeResult().Capabilities.Experimental
	if _, ok := experimental["claude/channel/permission"]; ok {
		t.Fatalf("experimental capabilities = %#v, want claude/channel/permission omitted", experimental)
	}
}

func TestServerSendChannelNotificationWritesExpectedJSON(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	server := NewServerWithOptions(nil, nil, Options{
		Output: &output,
		Channel: ChannelOptions{
			Enabled: true,
		},
	})

	err := server.SendChannelNotification("hello", map[string]any{
		"chat_id":    "chan-1",
		"message_id": "msg-1",
		"user":       "alice",
		"user_id":    "user-1",
		"ts":         "2026-04-17T12:00:00Z",
	})
	if err != nil {
		t.Fatalf("SendChannelNotification() error = %v", err)
	}

	want := "{\"jsonrpc\":\"2.0\",\"method\":\"notifications/claude/channel\",\"params\":{\"content\":\"hello\",\"meta\":{\"chat_id\":\"chan-1\",\"message_id\":\"msg-1\",\"user\":\"alice\",\"user_id\":\"user-1\",\"ts\":\"2026-04-17T12:00:00Z\"}}}\n"
	if got := output.String(); got != want {
		t.Fatalf("notification = %q, want %q", got, want)
	}
}

func TestServerWaitUntilReadyReturnsAfterInitialization(t *testing.T) {
	t.Parallel()

	server := NewServerWithOptions(nil, nil, Options{})

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Raw().Run(ctx, serverTransport)
	}()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "test-client",
		Version: buildinfo.Version,
	}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer func() {
		_ = session.Close()
		cancel()
		<-serverDone
	}()

	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := server.WaitUntilReady(waitCtx); err != nil {
		t.Fatalf("WaitUntilReady() error = %v", err)
	}
}

package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
)

// --- Types ---

// StdioClient runs a stdio hook command for each envelope.
type StdioClient struct {
	command []string
	stderr  io.Writer
}

// --- Constructors ---

// NewStdioClient creates a stdio hook client.
func NewStdioClient(command []string, stderr io.Writer) (*StdioClient, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("hook stdio command must not be empty")
	}

	cloned := append([]string(nil), command...)
	return &StdioClient{
		command: cloned,
		stderr:  stderr,
	}, nil
}

// --- Decisions ---

// Decide sends an envelope to the stdio hook command.
func (c *StdioClient) Decide(ctx context.Context, envelope Envelope) (Response, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Response{}, fmt.Errorf("failed to encode hook request: %w", err)
	}
	payload = append(payload, '\n')

	command := exec.CommandContext(ctx, c.command[0], c.command[1:]...)
	command.Stdin = bytes.NewReader(payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	if err := command.Run(); err != nil {
		if stderr.Len() > 0 && c.stderr != nil {
			_, _ = c.stderr.Write(stderr.Bytes())
		}
		if ctx.Err() != nil {
			return Response{}, fmt.Errorf("hook command timed out: %w", ctx.Err())
		}
		return Response{}, fmt.Errorf("hook command failed: %w", err)
	}

	if stderr.Len() > 0 && c.stderr != nil {
		_, _ = c.stderr.Write(stderr.Bytes())
	}

	return DecodeResponse(stdout.Bytes())
}

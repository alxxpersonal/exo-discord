package hook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// --- Types ---

// HTTPClient posts envelopes to an HTTP hook.
type HTTPClient struct {
	url     string
	headers map[string]string
	client  *http.Client
}

// --- Constructors ---

// NewHTTPClient creates an HTTP hook client.
func NewHTTPClient(url string, timeout time.Duration, headers map[string]string) (*HTTPClient, error) {
	if url == "" {
		return nil, fmt.Errorf("hook http url must not be empty")
	}
	if timeout <= 0 {
		return nil, fmt.Errorf("hook http timeout must be greater than zero")
	}

	clonedHeaders := make(map[string]string, len(headers))
	for key, value := range headers {
		clonedHeaders[key] = value
	}

	return &HTTPClient{
		url:     url,
		headers: clonedHeaders,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// --- Decisions ---

// Decide sends an envelope to the HTTP hook.
func (c *HTTPClient) Decide(ctx context.Context, envelope Envelope) (Response, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Response{}, fmt.Errorf("failed to encode hook request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("failed to build hook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	for key, value := range c.headers {
		request.Header.Set(key, value)
	}

	response, err := c.client.Do(request)
	if err != nil {
		return Response{}, fmt.Errorf("hook request failed: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNoContent {
		return Response{Decision: DecisionSkip}, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return Response{}, fmt.Errorf("hook returned status %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return Response{}, fmt.Errorf("failed to read hook response: %w", err)
	}

	return DecodeResponse(body)
}

package userinstall

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// --- Types ---

// CurrentUser stores the subset of GET /users/@me used by the user-install session.
type CurrentUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	GlobalName    string `json:"global_name"`
	Discriminator string `json:"discriminator"`
}

// PartialGuild stores the subset of GET /users/@me/guilds returned for a user.
type PartialGuild struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Owner bool   `json:"owner"`
}

// RESTClient performs Discord REST calls with a user-auth bearer token.
// It honors X-RateLimit-* headers and respects Retry-After on 429 responses.
type RESTClient struct {
	http        *http.Client
	apiBase     string
	bearer      string
	mu          sync.Mutex
	bucketReset map[string]time.Time
	clock       func() time.Time
	sleep       func(time.Duration)
}

// --- Constructors ---

// NewRESTClient creates a REST client for a single user-auth bearer token.
func NewRESTClient(httpClient *http.Client, apiBase string, bearer string) *RESTClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if apiBase == "" {
		apiBase = APIBase
	}
	return &RESTClient{
		http:        httpClient,
		apiBase:     strings.TrimRight(apiBase, "/"),
		bearer:      bearer,
		bucketReset: make(map[string]time.Time),
		clock:       time.Now,
		sleep:       time.Sleep,
	}
}

// --- User Endpoints ---

// CurrentUser returns the authorized user via GET /users/@me.
func (c *RESTClient) CurrentUser(ctx context.Context) (CurrentUser, error) {
	var user CurrentUser
	if err := c.do(ctx, http.MethodGet, "/users/@me", "users-me", &user); err != nil {
		return CurrentUser{}, err
	}
	return user, nil
}

// CurrentGuilds returns the authorized user guilds via GET /users/@me/guilds.
func (c *RESTClient) CurrentGuilds(ctx context.Context) ([]PartialGuild, error) {
	var guilds []PartialGuild
	if err := c.do(ctx, http.MethodGet, "/users/@me/guilds", "users-me-guilds", &guilds); err != nil {
		return nil, err
	}
	return guilds, nil
}

// --- HTTP ---

func (c *RESTClient) do(ctx context.Context, method string, path string, bucket string, out any) error {
	if strings.TrimSpace(c.bearer) == "" {
		return fmt.Errorf("user-auth bearer token is required")
	}

	if err := c.waitForBucket(ctx, bucket); err != nil {
		return fmt.Errorf("wait for rate limit bucket %q: %w", bucket, err)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, nil)
	if err != nil {
		return fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.bearer)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	c.recordRateLimit(bucket, resp.Header)

	if resp.StatusCode == http.StatusTooManyRequests {
		retry := parseRetryAfter(resp.Header.Get("Retry-After"))
		if retry > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retry):
			}
		}
		return fmt.Errorf("rate limited by discord on %s %s", method, path)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized: bearer token rejected")
	}
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("discord %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if out == nil {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read %s %s body: %w", method, path, err)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode %s %s body: %w", method, path, err)
	}
	return nil
}

// --- Rate Limit Helpers ---

func (c *RESTClient) recordRateLimit(bucket string, header http.Header) {
	remaining := header.Get("X-RateLimit-Remaining")
	resetAfter := header.Get("X-RateLimit-Reset-After")
	if remaining == "" || resetAfter == "" {
		return
	}
	if remaining != "0" {
		return
	}

	seconds, err := strconv.ParseFloat(resetAfter, 64)
	if err != nil || seconds <= 0 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.bucketReset[bucket] = c.clock().Add(time.Duration(seconds * float64(time.Second)))
}

func (c *RESTClient) waitForBucket(ctx context.Context, bucket string) error {
	c.mu.Lock()
	reset, ok := c.bucketReset[bucket]
	c.mu.Unlock()
	if !ok {
		return nil
	}

	defer func() {
		c.mu.Lock()
		delete(c.bucketReset, bucket)
		c.mu.Unlock()
	}()

	wait := reset.Sub(c.clock())
	if wait <= 0 {
		return nil
	}

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(value, 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

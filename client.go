// Package remedy provides a Go client for the BMC Remedy AR System REST API.
//
// The client handles authentication, request serialization (to avoid session
// conflicts), rate limiting, and provides a type-safe interface for interacting
// with Remedy forms and entries.
//
// Basic usage:
//
//	client := remedy.New("https://remedy.example.com:8443",
//	    remedy.WithTimeout(30*time.Second),
//	    remedy.WithRateLimit(10), // 10 requests/second
//	)
//
//	err := client.Login(ctx, "username", "password")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Logout(ctx)
//
//	entries, err := client.Entries().List(ctx, "HPD:Help Desk",
//	    remedy.WithQualification("'Status' = \"Open\""),
//	    remedy.WithLimit(100),
//	)
package remedy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tphakala/go-remedy/internal/queue"
	"github.com/tphakala/go-remedy/internal/ratelimit"
)

const (
	defaultTimeout          = 30 * time.Second
	defaultTokenLifetime    = 1 * time.Hour
	defaultRefreshThreshold = 5 * time.Minute
	apiBasePath             = "/api/arsys/v1"
	jwtBasePath             = "/api/jwt"
	authHeaderPrefix        = "AR-JWT "
)

// credentials stores authentication info for automatic token refresh.
// Fields are private to prevent accidental logging via %+v or reflection.
//
// TODO(go1.26): Consider using runtime/secret package for credential storage
// when Go 1.26 is available. See: https://go.dev/doc/go1.26#new-experimental-runtimesecret-package
type credentials struct {
	username   string
	password   string
	authString string
}

// Client is a BMC Remedy REST API client.
// It handles authentication, request serialization, and rate limiting.
//
// TODO(go1.26): Consider using runtime/secret package for token storage
// when Go 1.26 is available. See: https://go.dev/doc/go1.26#new-experimental-runtimesecret-package
type Client struct {
	baseURL     string
	httpClient  HTTPDoer
	timeout     time.Duration
	rateLimiter *ratelimit.Limiter
	queue       *queue.Queue

	// Token management
	token       string
	tokenExpiry time.Time
	tokenMu     sync.RWMutex

	// Credential storage for auto-refresh
	credentials   *credentials
	credentialsMu sync.RWMutex

	// Token refresh configuration
	tokenLifetime    time.Duration
	refreshThreshold time.Duration
	autoRefresh      bool
	refreshMu        sync.Mutex // serializes token refresh attempts

	entries     *entryService
	attachments *attachmentService
}

// New creates a new Remedy client with the specified base URL and options.
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:          strings.TrimSuffix(baseURL, "/"),
		httpClient:       &http.Client{},
		timeout:          defaultTimeout,
		tokenLifetime:    defaultTokenLifetime,
		refreshThreshold: defaultRefreshThreshold,
		autoRefresh:      true,
		queue:            queue.New(),
	}

	for _, opt := range opts {
		opt(c)
	}

	c.entries = &entryService{client: c}
	c.attachments = &attachmentService{client: c}

	return c
}

// Entries returns the entry service for CRUD operations on form entries.
func (c *Client) Entries() EntryServicer {
	return c.entries
}

// Attachments returns the attachment service for file operations.
func (c *Client) Attachments() AttachmentServicer {
	return c.attachments
}

// Close releases resources associated with the client.
func (c *Client) Close() {
	c.queue.Close()
}

// getToken returns the current auth token (thread-safe).
func (c *Client) getToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	return c.token
}

// setToken sets the auth token (thread-safe).
func (c *Client) setToken(token string) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	c.token = token
}

// setTokenWithExpiry sets the auth token and its expiry time (thread-safe).
func (c *Client) setTokenWithExpiry(token string, expiry time.Time) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	c.token = token
	c.tokenExpiry = expiry
}

// getTokenExpiry returns the token expiry time (thread-safe).
func (c *Client) getTokenExpiry() time.Time {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	return c.tokenExpiry
}

// storeCredentials saves credentials for automatic token refresh.
func (c *Client) storeCredentials(username, password, authString string) {
	c.credentialsMu.Lock()
	defer c.credentialsMu.Unlock()

	c.credentials = &credentials{
		username:   username,
		password:   password,
		authString: authString,
	}
}

// hasCredentials returns true if credentials are stored for auto-refresh.
func (c *Client) hasCredentials() bool {
	c.credentialsMu.RLock()
	defer c.credentialsMu.RUnlock()

	return c.credentials != nil
}

// ClearCredentials removes stored credentials from memory.
// After calling this, automatic token refresh will be disabled.
func (c *Client) ClearCredentials() {
	c.credentialsMu.Lock()
	defer c.credentialsMu.Unlock()

	c.credentials = nil
}

// tokenNeedsRefresh returns true if the token is missing or near expiry.
func (c *Client) tokenNeedsRefresh() bool {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()

	return c.tokenNeedsRefreshLocked()
}

// tokenNeedsRefreshLocked checks if token needs refresh (caller must hold lock).
func (c *Client) tokenNeedsRefreshLocked() bool {
	if c.token == "" {
		return true
	}

	return time.Now().Add(c.refreshThreshold).After(c.tokenExpiry)
}

// ensureValidToken checks and refreshes the token if needed.
// This uses double-check locking with a separate refresh mutex to prevent
// concurrent refresh attempts while allowing concurrent token reads.
func (c *Client) ensureValidToken(ctx context.Context) error {
	// Fast path: check with read lock
	if !c.tokenNeedsRefresh() {
		return nil
	}

	// Check if auto-refresh is enabled
	if !c.autoRefresh {
		return ErrNoCredentials
	}

	// Acquire refresh mutex to serialize refresh attempts
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	// Double-check after acquiring refresh lock
	if !c.tokenNeedsRefresh() {
		return nil // Another goroutine already refreshed
	}

	return c.refreshToken(ctx)
}

// refreshToken performs token refresh using stored credentials.
func (c *Client) refreshToken(ctx context.Context) error {
	c.credentialsMu.RLock()
	creds := c.credentials
	c.credentialsMu.RUnlock()

	if creds == nil {
		return ErrNoCredentials
	}

	// Perform login - this will update the token atomically via setTokenWithExpiry
	return c.loginInternal(ctx, creds.username, creds.password, creds.authString)
}

// acquireAndRateLimit acquires the request queue and applies rate limiting.
// It also ensures the token is valid before proceeding.
func (c *Client) acquireAndRateLimit(ctx context.Context) error {
	// Ensure valid token before acquiring queue
	if err := c.ensureValidToken(ctx); err != nil {
		return fmt.Errorf("ensuring valid token: %w", err)
	}

	if err := c.queue.Acquire(ctx); err != nil {
		return fmt.Errorf("acquiring request queue: %w", err)
	}

	if c.rateLimiter != nil {
		if err := c.rateLimiter.Wait(ctx); err != nil {
			c.queue.Release()
			return fmt.Errorf("rate limit: %w", err)
		}
	}

	return nil
}

// newRequest creates a new HTTP request with context and auth header.
// The caller is responsible for calling the returned cancel function.
func (c *Client) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, context.CancelFunc, error) {
	reqURL := c.baseURL + path

	ctx, cancel := context.WithTimeout(ctx, c.timeout)

	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}

	token := c.getToken()
	if token != "" {
		req.Header.Set("Authorization", authHeaderPrefix+token)
	}

	return req, cancel, nil
}

// newJSONRequest creates a new HTTP request with JSON body and headers.
// The caller is responsible for calling the returned cancel function.
func (c *Client) newJSONRequest(ctx context.Context, method, path string, body any) (*http.Request, context.CancelFunc, error) {
	var bodyReader io.Reader

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, nil, fmt.Errorf("marshaling request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, cancel, err := c.newRequest(ctx, method, path, bodyReader)
	if err != nil {
		return nil, nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	return req, cancel, nil
}

// do executes an HTTP request and returns the response.
// The caller is responsible for closing the response body and calling cancel.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing request: %w", err)
	}

	return resp, nil
}

// doAndDecode executes a request and decodes the JSON response.
func (c *Client) doAndDecode(req *http.Request, cancel context.CancelFunc, target any) error {
	defer cancel()

	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	return c.handleResponse(resp, target)
}

// handleResponse checks the response status and decodes the body.
func (c *Client) handleResponse(resp *http.Response, target any) error {
	if resp.StatusCode >= http.StatusBadRequest {
		return c.parseAPIError(resp)
	}

	if target == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}

	return nil
}

// parseAPIError extracts error information from an error response.
func (c *Client) parseAPIError(resp *http.Response) error {
	var apiErrors []apiErrorResponse

	if err := json.NewDecoder(resp.Body).Decode(&apiErrors); err != nil {
		// If we can't parse the error, return a generic one
		return &APIError{
			StatusCode:  resp.StatusCode,
			MessageType: "ERROR",
			MessageText: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status),
		}
	}

	if len(apiErrors) == 0 {
		return &APIError{
			StatusCode:  resp.StatusCode,
			MessageType: "ERROR",
			MessageText: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, resp.Status),
		}
	}

	// Return the first error
	e := apiErrors[0]

	return &APIError{
		StatusCode:          resp.StatusCode,
		MessageType:         e.MessageType,
		MessageText:         e.MessageText,
		MessageAppendedText: e.MessageAppendedText,
		MessageNumber:       e.MessageNumber,
	}
}

// entryPath builds the API path for entry operations.
func entryPath(form string) string {
	return apiBasePath + "/entry/" + url.PathEscape(form)
}

// entryIDPath builds the API path for a specific entry.
func entryIDPath(form, entryID string) string {
	return entryPath(form) + "/" + url.PathEscape(entryID)
}


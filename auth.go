package remedy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxTokenSize is the maximum allowed size for JWT token responses.
// JWT tokens are typically under 8KB; 64KB provides ample headroom.
const maxTokenSize = 64 * 1024

// ErrTokenTooLarge is returned when a token response exceeds maxTokenSize.
var ErrTokenTooLarge = errors.New("remedy: token too large")

// Login authenticates with the Remedy server using username and password.
// The JWT token is stored internally and used for subsequent requests.
// Credentials are stored for automatic token refresh.
func (c *Client) Login(ctx context.Context, username, password string) error {
	return c.LoginWithAuth(ctx, username, password, "")
}

// LoginWithAuth authenticates with an additional authentication string.
// This is used for servers that require additional authentication context.
// Credentials are stored for automatic token refresh.
func (c *Client) LoginWithAuth(ctx context.Context, username, password, authString string) error {
	// Use queue for initial login (not called during refresh)
	if err := c.loginAcquireQueue(ctx); err != nil {
		return err
	}
	defer c.queue.Release()

	if err := c.loginInternal(ctx, username, password, authString); err != nil {
		return err
	}

	// Store credentials for automatic token refresh
	c.storeCredentials(username, password, authString)

	return nil
}

// loginAcquireQueue acquires queue and rate limiter without token check.
// Used for initial login to avoid circular dependency.
func (c *Client) loginAcquireQueue(ctx context.Context) error {
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

// loginInternal performs the actual login HTTP request.
// It does not acquire the queue (caller must handle that).
func (c *Client) loginInternal(ctx context.Context, username, password, authString string) error {
	form := url.Values{}
	form.Set("username", username)
	form.Set("password", password)
	if authString != "" {
		form.Set("authString", authString)
	}

	req, cancel, err := c.newRequest(ctx, http.MethodPost, jwtBasePath+"/login", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("creating login request: %w", err)
	}
	defer cancel()

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return c.parseAPIError(resp)
	}

	// Limit read to prevent memory exhaustion from malicious servers
	limitedReader := io.LimitReader(resp.Body, maxTokenSize+1)
	token, err := io.ReadAll(limitedReader)
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	if len(token) > maxTokenSize {
		return ErrTokenTooLarge
	}

	// Set token with expiry based on configured lifetime
	c.setTokenWithExpiry(strings.TrimSpace(string(token)), time.Now().Add(c.tokenLifetime))

	return nil
}

// Logout terminates the current session and clears the stored token.
func (c *Client) Logout(ctx context.Context) error {
	token := c.getToken()
	if token == "" {
		return nil // Already logged out
	}

	if err := c.acquireAndRateLimit(ctx); err != nil {
		return err
	}
	defer c.queue.Release()

	req, cancel, err := c.newRequest(ctx, http.MethodPost, jwtBasePath+"/logout", nil)
	if err != nil {
		return fmt.Errorf("creating logout request: %w", err)
	}
	defer cancel()

	resp, err := c.do(req)
	if err != nil {
		// Clear token even if request fails
		c.setToken("")
		return fmt.Errorf("logout request: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Clear token regardless of response
	c.setToken("")

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return c.parseAPIError(resp)
	}

	return nil
}

// IsAuthenticated returns true if the client has a valid token.
// Note: This only checks if a token exists, not if it's still valid.
func (c *Client) IsAuthenticated() bool {
	return c.getToken() != ""
}

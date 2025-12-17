package remedy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testLoginPath = "/api/jwt/login"

func TestClient_Login_StoresCredentials(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "testuser", "testpass")
	require.NoError(t, err)

	// Credentials should be stored for auto-refresh
	assert.True(t, client.hasCredentials(), "credentials should be stored after login")
}

func TestClient_LoginWithAuth_StoresCredentials(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.LoginWithAuth(t.Context(), "testuser", "testpass", "authstring")
	require.NoError(t, err)

	assert.True(t, client.hasCredentials(), "credentials should be stored after login")
}

func TestClient_Login_SetsTokenExpiry(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))
	before := time.Now()

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Token expiry should be set to approximately tokenLifetime from now
	expiry := client.getTokenExpiry()
	expectedExpiry := before.Add(defaultTokenLifetime)

	// Allow 1 second tolerance
	assert.WithinDuration(t, expectedExpiry, expiry, time.Second,
		"token expiry should be set to tokenLifetime from login time")
}

func TestClient_WithTokenLifetime(t *testing.T) {
	customLifetime := 30 * time.Minute

	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(customLifetime),
	)
	before := time.Now()

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	expiry := client.getTokenExpiry()
	expectedExpiry := before.Add(customLifetime)

	assert.WithinDuration(t, expectedExpiry, expiry, time.Second,
		"token expiry should use custom lifetime")
}

func TestClient_WithRefreshThreshold(t *testing.T) {
	customThreshold := 10 * time.Minute

	client := New("https://remedy.example.com",
		WithRefreshThreshold(customThreshold),
	)

	assert.Equal(t, customThreshold, client.refreshThreshold,
		"refresh threshold should be configurable")
}

func TestClient_TokenNeedsRefresh_FreshToken(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Fresh token should not need refresh
	assert.False(t, client.tokenNeedsRefresh(),
		"fresh token should not need refresh")
}

func TestClient_TokenNeedsRefresh_NearExpiry(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	// Short lifetime and threshold to test near-expiry behavior
	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(10*time.Second),
		WithRefreshThreshold(8*time.Second),
	)

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Wait until we're within the refresh threshold
	time.Sleep(3 * time.Second)

	assert.True(t, client.tokenNeedsRefresh(),
		"token near expiry should need refresh")
}

func TestClient_TokenNeedsRefresh_NoToken(t *testing.T) {
	client := New("https://remedy.example.com")

	// No token means refresh needed (will fail without credentials)
	assert.True(t, client.tokenNeedsRefresh(),
		"client without token should indicate refresh needed")
}

func TestClient_EnsureValidToken_RefreshesExpiredToken(t *testing.T) {
	loginCount := atomic.Int32{}

	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				loginCount.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("new-token"))),
					Header:     make(http.Header),
				}, nil
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	// Very short lifetime to trigger refresh
	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(50*time.Millisecond),
	)

	// Initial login
	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)
	assert.Equal(t, int32(1), loginCount.Load())

	// Wait for token to need refresh
	time.Sleep(60 * time.Millisecond)

	// Make a request - should trigger automatic refresh
	_, err = client.Entries().Get(t.Context(), "Form", "ID")
	require.NoError(t, err)

	// Should have called login again for refresh
	assert.Equal(t, int32(2), loginCount.Load(),
		"expired token should trigger automatic refresh")
}

func TestClient_EnsureValidToken_ConcurrentRefresh(t *testing.T) {
	loginCount := atomic.Int32{}

	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				loginCount.Add(1)
				// Simulate slow login
				time.Sleep(50 * time.Millisecond)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("new-token"))),
					Header:     make(http.Header),
				}, nil
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(90*time.Millisecond),
	)

	// Initial login
	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Wait for token to need refresh
	time.Sleep(20 * time.Millisecond)

	// Start multiple concurrent requests
	var wg sync.WaitGroup
	const numRequests = 5

	for range numRequests {
		wg.Go(func() {
			_, _ = client.Entries().Get(t.Context(), "Form", "ID")
		})
	}

	wg.Wait()

	// Only one refresh should have occurred despite concurrent requests
	// Initial login (1) + one refresh (1) = 2
	assert.Equal(t, int32(2), loginCount.Load(),
		"concurrent requests should only trigger one refresh")
}

func TestClient_EnsureValidToken_NoCredentials(t *testing.T) {
	client := New("https://remedy.example.com")

	// Without credentials, ensureValidToken should return error
	err := client.ensureValidToken(t.Context())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoCredentials,
		"should return ErrNoCredentials when no credentials stored")
}

func TestClient_ClearCredentials(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)
	require.True(t, client.hasCredentials())

	client.ClearCredentials()

	assert.False(t, client.hasCredentials(),
		"credentials should be cleared after ClearCredentials")
}

func TestClient_ClearCredentials_DisablesAutoRefresh(t *testing.T) {
	loginCount := atomic.Int32{}

	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				loginCount.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
					Header:     make(http.Header),
				}, nil
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(50*time.Millisecond),
	)

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Clear credentials
	client.ClearCredentials()

	// Wait for token to expire
	time.Sleep(60 * time.Millisecond)

	// Attempt request - should fail because credentials are cleared
	_, err = client.Entries().Get(t.Context(), "Form", "ID")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrNoCredentials,
		"request should fail after credentials cleared and token expired")
}

func TestClient_WithAutoRefresh_Disabled(t *testing.T) {
	loginCount := atomic.Int32{}

	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				loginCount.Add(1)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
					Header:     make(http.Header),
				}, nil
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(50*time.Millisecond),
		WithAutoRefresh(false),
	)

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Wait for token to need refresh
	time.Sleep(60 * time.Millisecond)

	// Request should fail because auto-refresh is disabled
	_, err = client.Entries().Get(t.Context(), "Form", "ID")
	require.Error(t, err)

	// Login should only have been called once (initial)
	assert.Equal(t, int32(1), loginCount.Load(),
		"auto-refresh disabled should not trigger refresh")
}

func TestClient_RefreshToken_Failure(t *testing.T) {
	loginCount := atomic.Int32{}

	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				count := loginCount.Add(1)
				if count == 1 {
					// First login succeeds
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
						Header:     make(http.Header),
					}, nil
				}
				// Subsequent logins fail
				return newMockResponse(http.StatusUnauthorized, []apiErrorResponse{
					{MessageType: "ERROR", MessageText: "Auth failed", MessageNumber: 623},
				}), nil
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(50*time.Millisecond),
	)

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Wait for token to need refresh
	time.Sleep(60 * time.Millisecond)

	// Request should fail because refresh fails
	_, err = client.Entries().Get(t.Context(), "Form", "ID")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestClient_RefreshToken_ContextCancellation(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == testLoginPath {
				// Simulate slow login for first call, then check context
				select {
				case <-req.Context().Done():
					return nil, req.Context().Err()
				case <-time.After(100 * time.Millisecond):
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
						Header:     make(http.Header),
					}, nil
				}
			}
			return newMockResponse(http.StatusOK, Entry{}), nil
		},
	}

	client := New("https://remedy.example.com",
		WithHTTPClient(mock),
		WithTokenLifetime(100*time.Millisecond),
		WithRefreshThreshold(50*time.Millisecond),
	)

	// First login
	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	// Wait for token to need refresh
	time.Sleep(60 * time.Millisecond)

	// Create a context that we'll cancel
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	// Request should fail due to context cancellation
	_, err = client.Entries().Get(ctx, "Form", "ID")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

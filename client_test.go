package remedy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHTTPClient implements HTTPDoer for testing.
type mockHTTPClient struct {
	doFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.doFunc(req)
}

func newMockResponse(statusCode int, body any) *http.Response {
	var bodyReader io.ReadCloser

	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			panic("newMockResponse: " + err.Error())
		}
		bodyReader = io.NopCloser(bytes.NewReader(data))
	} else {
		bodyReader = io.NopCloser(bytes.NewReader(nil))
	}

	return &http.Response{
		StatusCode: statusCode,
		Body:       bodyReader,
		Header:     make(http.Header),
	}
}

func TestNew(t *testing.T) {
	client := New("https://remedy.example.com")

	assert.NotNil(t, client)
	assert.Equal(t, "https://remedy.example.com", client.baseURL)
	assert.NotNil(t, client.Entries())
	assert.NotNil(t, client.Attachments())
}

func TestNew_WithTrailingSlash(t *testing.T) {
	client := New("https://remedy.example.com/")

	assert.Equal(t, "https://remedy.example.com", client.baseURL)
}

func TestClient_Login(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Contains(t, req.URL.Path, "/api/jwt/login")
			assert.Equal(t, "application/x-www-form-urlencoded", req.Header.Get("Content-Type"))

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("test-jwt-token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "pass")

	require.NoError(t, err)
	assert.True(t, client.IsAuthenticated())
	assert.Equal(t, "test-jwt-token", client.getToken())
}

func TestClient_Login_Error(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return newMockResponse(http.StatusUnauthorized, []apiErrorResponse{
				{
					MessageType:   "ERROR",
					MessageText:   "Authentication failed",
					MessageNumber: 623,
				},
			}), nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "wrong")

	require.Error(t, err)
	assert.False(t, client.IsAuthenticated())

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
}

func TestClient_Logout(t *testing.T) {
	loginCalled := false
	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if !loginCalled {
				loginCalled = true
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("token"))),
					Header:     make(http.Header),
				}, nil
			}

			assert.Equal(t, http.MethodPost, req.Method)
			assert.Contains(t, req.URL.Path, "/api/jwt/logout")
			assert.Equal(t, "AR-JWT token", req.Header.Get("Authorization"))

			return newMockResponse(http.StatusNoContent, nil), nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)
	require.True(t, client.IsAuthenticated())

	err = client.Logout(t.Context())
	require.NoError(t, err)
	assert.False(t, client.IsAuthenticated())
}

func TestClient_Logout_NotAuthenticated(t *testing.T) {
	client := New("https://remedy.example.com")

	// Logout when not authenticated should succeed silently
	err := client.Logout(t.Context())
	require.NoError(t, err)
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      *APIError
		expected string
	}{
		{
			name: "with appended text",
			err: &APIError{
				StatusCode:          404,
				MessageType:         "ERROR",
				MessageText:         "Entry does not exist",
				MessageAppendedText: "ID: 123",
				MessageNumber:       302,
			},
			expected: "remedy: ERROR (302): Entry does not exist - ID: 123",
		},
		{
			name: "without appended text",
			err: &APIError{
				StatusCode:    401,
				MessageType:   "ERROR",
				MessageText:   "Authentication failed",
				MessageNumber: 623,
			},
			expected: "remedy: ERROR (623): Authentication failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestAPIError_Is(t *testing.T) {
	tests := []struct {
		name       string
		err        *APIError
		target     error
		shouldMatch bool
	}{
		{
			name:       "unauthorized",
			err:        &APIError{StatusCode: 401},
			target:     ErrUnauthorized,
			shouldMatch: true,
		},
		{
			name:       "forbidden",
			err:        &APIError{StatusCode: 403},
			target:     ErrForbidden,
			shouldMatch: true,
		},
		{
			name:       "not found",
			err:        &APIError{StatusCode: 404},
			target:     ErrNotFound,
			shouldMatch: true,
		},
		{
			name:       "no match",
			err:        &APIError{StatusCode: 500},
			target:     ErrUnauthorized,
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.shouldMatch, tt.err.Is(tt.target))
		})
	}
}

func TestBuildQueryParams(t *testing.T) {
	tests := []struct {
		name     string
		opts     []QueryOption
		expected map[string]string
	}{
		{
			name: "fields",
			opts: []QueryOption{WithFields("Status", "Priority")},
			expected: map[string]string{
				"fields": "values(Status,Priority)",
			},
		},
		{
			name: "qualification",
			opts: []QueryOption{WithQualification("'Status' = \"Open\"")},
			expected: map[string]string{
				"q": "'Status' = \"Open\"",
			},
		},
		{
			name: "sort ascending",
			opts: []QueryOption{WithSort("Priority", SortAsc)},
			expected: map[string]string{
				"sort": "Priority",
			},
		},
		{
			name: "sort descending",
			opts: []QueryOption{WithSort("Priority", SortDesc)},
			expected: map[string]string{
				"sort": "Priority.desc",
			},
		},
		{
			name: "pagination",
			opts: []QueryOption{WithLimit(100), WithOffset(50)},
			expected: map[string]string{
				"limit":  "100",
				"offset": "50",
			},
		},
		{
			name: "expand",
			opts: []QueryOption{WithExpand("assoc1", "assoc2")},
			expected: map[string]string{
				"expand": "assoc1,assoc2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := buildQueryParams(tt.opts)
			for key, value := range tt.expected {
				assert.Equal(t, value, params.Get(key))
			}
		})
	}
}

func TestClient_ContextCancellation(t *testing.T) {
	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader([]byte("token"))),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := client.Login(ctx, "user", "pass")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestBuildQueryParams_RejectsNegativeLimit(t *testing.T) {
	params := buildQueryParams([]QueryOption{WithLimit(-1)})

	// Negative limit should not be included in params
	assert.Empty(t, params.Get("limit"), "negative limit should not be included")
}

func TestBuildQueryParams_RejectsNegativeOffset(t *testing.T) {
	params := buildQueryParams([]QueryOption{WithOffset(-5)})

	// Negative offset should not be included in params
	assert.Empty(t, params.Get("offset"), "negative offset should not be included")
}

func TestBuildQueryParams_AcceptsZeroValues(t *testing.T) {
	params := buildQueryParams([]QueryOption{WithLimit(0), WithOffset(0)})

	// Zero values should not be included (existing behavior for limit > 0)
	assert.Empty(t, params.Get("limit"))
	assert.Empty(t, params.Get("offset"))
}

func TestClient_Login_RejectsOversizedToken(t *testing.T) {
	// A malicious server could try to exhaust memory by sending huge response
	// Token response should be limited to a reasonable size
	oversizedToken := bytes.Repeat([]byte("a"), 2*1024*1024) // 2MB

	mock := &mockHTTPClient{
		doFunc: func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewReader(oversizedToken)),
				Header:     make(http.Header),
			}, nil
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))

	err := client.Login(t.Context(), "user", "pass")

	// Should reject oversized token
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token too large")
}

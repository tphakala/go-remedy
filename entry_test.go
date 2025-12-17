package remedy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthenticatedClient(t *testing.T, doFunc func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()

	callCount := 0
	mock := &mockHTTPClient{
		doFunc: func(req *http.Request) (*http.Response, error) {
			if callCount == 0 {
				callCount++
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader([]byte("test-token"))),
					Header:     make(http.Header),
				}, nil
			}
			return doFunc(req)
		},
	}

	client := New("https://remedy.example.com", WithHTTPClient(mock))
	err := client.Login(t.Context(), "user", "pass")
	require.NoError(t, err)

	return client
}

func TestEntryService_Get(t *testing.T) {
	expectedEntry := Entry{
		Values: map[string]any{
			"Request ID": "REQ000001",
			"Status":     "Open",
		},
	}

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, req.Method)
		// req.URL.Path is already decoded; check for the decoded form name
		assert.Contains(t, req.URL.Path, "/api/arsys/v1/entry/HPD:Help Desk/REQ000001")
		assert.Equal(t, "AR-JWT test-token", req.Header.Get("Authorization"))

		return newMockResponse(http.StatusOK, expectedEntry), nil
	})

	entry, err := client.Entries().Get(t.Context(), "HPD:Help Desk", "REQ000001")

	require.NoError(t, err)
	assert.Equal(t, "REQ000001", entry.Values["Request ID"])
	assert.Equal(t, "Open", entry.Values["Status"])
}

func TestEntryService_Get_WithOptions(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		// Use Query().Get() to check the decoded parameter value
		assert.Equal(t, "values(Status,Priority)", req.URL.Query().Get("fields"))

		return newMockResponse(http.StatusOK, Entry{}), nil
	})

	_, err := client.Entries().Get(t.Context(), "Form", "ID", WithFields("Status", "Priority"))
	require.NoError(t, err)
}

func TestEntryService_Get_NotFound(t *testing.T) {
	client := setupAuthenticatedClient(t, func(_ *http.Request) (*http.Response, error) {
		return newMockResponse(http.StatusNotFound, []apiErrorResponse{
			{
				MessageType:   "ERROR",
				MessageText:   "Entry does not exist",
				MessageNumber: 302,
			},
		}), nil
	})

	_, err := client.Entries().Get(t.Context(), "Form", "invalid-id")

	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	assert.True(t, apiErr.Is(ErrNotFound))
}

func TestEntryService_List(t *testing.T) {
	expectedList := EntryList{
		Entries: []Entry{
			{Values: map[string]any{"Request ID": "REQ000001"}},
			{Values: map[string]any{"Request ID": "REQ000002"}},
		},
	}

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Contains(t, req.URL.Path, "/api/arsys/v1/entry/")

		return newMockResponse(http.StatusOK, expectedList), nil
	})

	list, err := client.Entries().List(t.Context(), "HPD:Help Desk")

	require.NoError(t, err)
	assert.Len(t, list.Entries, 2)
}

func TestEntryService_List_WithQualification(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Contains(t, req.URL.RawQuery, "q=")

		return newMockResponse(http.StatusOK, EntryList{}), nil
	})

	q := NewQuery().And("Status", "=", "Open").Build()
	_, err := client.Entries().List(t.Context(), "Form", WithQualification(q))

	require.NoError(t, err)
}

func TestEntryService_Create(t *testing.T) {
	expectedEntry := Entry{
		Values: map[string]any{
			"Request ID": "REQ000003",
			"Status":     "New",
		},
	}

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

		// Verify request body
		var body map[string]any
		err := json.NewDecoder(req.Body).Decode(&body)
		require.NoError(t, err)

		values, ok := body["values"].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "Test Summary", values["Summary"])

		return newMockResponse(http.StatusCreated, expectedEntry), nil
	})

	entry, err := client.Entries().Create(t.Context(), "HPD:Help Desk", map[string]any{
		"Summary": "Test Summary",
	})

	require.NoError(t, err)
	assert.Equal(t, "REQ000003", entry.Values["Request ID"])
}

func TestEntryService_Update(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPut, req.Method)
		assert.Contains(t, req.URL.Path, "/REQ000001")

		return newMockResponse(http.StatusNoContent, nil), nil
	})

	err := client.Entries().Update(t.Context(), "HPD:Help Desk", "REQ000001", map[string]any{
		"Status": "In Progress",
	})

	require.NoError(t, err)
}

func TestEntryService_Delete(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodDelete, req.Method)
		assert.Contains(t, req.URL.Path, "/REQ000001")

		return newMockResponse(http.StatusNoContent, nil), nil
	})

	err := client.Entries().Delete(t.Context(), "HPD:Help Desk", "REQ000001")
	require.NoError(t, err)
}

func TestEntryService_Delete_WithOption(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Contains(t, req.URL.RawQuery, "options=FORCE")

		return newMockResponse(http.StatusNoContent, nil), nil
	})

	err := client.Entries().Delete(t.Context(), "Form", "ID", DeleteOptionForce)
	require.NoError(t, err)
}

func TestEntryService_Delete_OptionIsURLEncoded(t *testing.T) {
	// Even though DeleteOption is a string type, any value with special chars
	// should be URL-encoded to prevent query parameter injection
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		// The raw query should have properly encoded option value
		// If someone passes "FORCE&evil=1", it should be encoded as "FORCE%26evil%3D1"
		rawQuery := req.URL.RawQuery
		assert.Equal(t, "options=FORCE%26evil%3D1", rawQuery,
			"delete option should be URL-encoded")

		return newMockResponse(http.StatusNoContent, nil), nil
	})

	// Simulate a potentially malicious option value
	maliciousOption := DeleteOption("FORCE&evil=1")
	err := client.Entries().Delete(t.Context(), "Form", "ID", maliciousOption)
	require.NoError(t, err)
}

func TestEntryService_Merge(t *testing.T) {
	expectedEntry := Entry{
		Values: map[string]any{"Request ID": "REQ000001"},
	}

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Contains(t, req.URL.Path, "/mergeEntry/")

		return newMockResponse(http.StatusOK, expectedEntry), nil
	})

	entry, err := client.Entries().Merge(t.Context(), "Form", map[string]any{
		"Summary": "Test",
	})

	require.NoError(t, err)
	assert.NotNil(t, entry)
}

func TestEntryService_Merge_URLEncodesFormName(t *testing.T) {
	// Form names with special characters should be URL-encoded
	// url.PathEscape encodes spaces but not colons (they're valid in paths)
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		escapedPath := req.URL.EscapedPath()
		// Space should be encoded as %20
		assert.Contains(t, escapedPath, "Help%20Desk",
			"space in form name should be URL-encoded, got: %s", escapedPath)

		return newMockResponse(http.StatusOK, Entry{}), nil
	})

	_, err := client.Entries().Merge(t.Context(), "HPD:Help Desk", map[string]any{
		"Summary": "Test",
	})

	require.NoError(t, err)
}

func TestEntryService_Merge_URLEncodesSlashInFormName(t *testing.T) {
	// Form names with slashes must be URL-encoded to prevent path traversal
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		escapedPath := req.URL.EscapedPath()
		// Slash should be encoded as %2F
		assert.Contains(t, escapedPath, "Form%2FName",
			"slash in form name should be URL-encoded, got: %s", escapedPath)

		return newMockResponse(http.StatusOK, Entry{}), nil
	})

	_, err := client.Entries().Merge(t.Context(), "Form/Name", map[string]any{
		"Summary": "Test",
	})

	require.NoError(t, err)
}

func TestEntryService_Get_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	_, err := client.Entries().Get(t.Context(), "", "REQ000001")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

func TestEntryService_Get_EmptyEntryIDReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	_, err := client.Entries().Get(t.Context(), "Form", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryService_List_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	_, err := client.Entries().List(t.Context(), "")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

func TestEntryService_Create_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	_, err := client.Entries().Create(t.Context(), "", map[string]any{"Summary": "Test"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

func TestEntryService_Update_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	err := client.Entries().Update(t.Context(), "", "REQ000001", map[string]any{"Status": "Open"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

func TestEntryService_Update_EmptyEntryIDReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	err := client.Entries().Update(t.Context(), "Form", "", map[string]any{"Status": "Open"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryService_Delete_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	err := client.Entries().Delete(t.Context(), "", "REQ000001")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

func TestEntryService_Delete_EmptyEntryIDReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	err := client.Entries().Delete(t.Context(), "Form", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyEntryID)
}

func TestEntryService_Merge_EmptyFormReturnsError(t *testing.T) {
	client := New("https://remedy.example.com")

	_, err := client.Entries().Merge(t.Context(), "", map[string]any{"Summary": "Test"})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyFormName)
}

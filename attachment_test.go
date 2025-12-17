package remedy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeTrackingReader tracks whether Close was called and fails reads after close.
// This simulates real HTTP response body behavior more accurately than NopCloser.
type closeTrackingReader struct {
	data   []byte
	pos    int
	closed bool
}

func newCloseTrackingReader(data []byte) *closeTrackingReader {
	return &closeTrackingReader{data: data}
}

func (r *closeTrackingReader) Read(p []byte) (n int, err error) {
	if r.closed {
		return 0, errors.New("read after close")
	}
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (r *closeTrackingReader) Close() error {
	r.closed = true
	return nil
}

func TestAttachmentService_Get_ErrorReturnsAPIErrorDetails(t *testing.T) {
	// This test verifies that when attachment Get fails, the actual API error
	// details are returned, not a generic "HTTP 404" message.
	// Uses closeTrackingReader to detect if body is read after being closed.
	expectedError := apiErrorResponse{
		MessageType:   "ERROR",
		MessageText:   "Attachment field not found",
		MessageNumber: 8892,
	}
	errorBody, err := json.Marshal([]apiErrorResponse{expectedError})
	require.NoError(t, err)

	client := setupAuthenticatedClient(t, func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       newCloseTrackingReader(errorBody),
			Header:     make(http.Header),
		}, nil
	})

	_, getErr := client.Attachments().Get(t.Context(), "Form", "EntryID", "NonexistentField")

	require.Error(t, getErr)

	var apiErr *APIError
	require.ErrorAs(t, getErr, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode)
	// This assertion will fail if the body was closed before parsing
	assert.Equal(t, "Attachment field not found", apiErr.MessageText)
	assert.Equal(t, 8892, apiErr.MessageNumber)
}

func TestAttachmentService_Get_Success(t *testing.T) {
	expectedData := []byte("attachment content")

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, req.Method)
		assert.Contains(t, req.URL.Path, "/attach/")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(expectedData)),
			Header:     make(http.Header),
		}, nil
	})

	reader, err := client.Attachments().Get(t.Context(), "Form", "EntryID", "AttachField")

	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, expectedData, data)
}

func TestAttachmentService_Upload_Success(t *testing.T) {
	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodPost, req.Method)
		assert.Contains(t, req.Header.Get("Content-Type"), "multipart/form-data")

		// Must drain request body to avoid blocking the pipe writer goroutine
		_, _ = io.Copy(io.Discard, req.Body)

		return newMockResponse(http.StatusNoContent, nil), nil
	})

	err := client.Attachments().Upload(
		t.Context(),
		"Form",
		"EntryID",
		"AttachField",
		"test.txt",
		bytes.NewReader([]byte("file content")),
	)

	require.NoError(t, err)
}

func TestAttachmentService_Upload_ErrorReturnsAPIErrorDetails(t *testing.T) {
	expectedError := apiErrorResponse{
		MessageType:   "ERROR",
		MessageText:   "File too large",
		MessageNumber: 9001,
	}

	client := setupAuthenticatedClient(t, func(req *http.Request) (*http.Response, error) {
		// Must drain request body to avoid blocking the pipe writer goroutine
		_, _ = io.Copy(io.Discard, req.Body)

		body, _ := json.Marshal([]apiErrorResponse{expectedError})
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})

	err := client.Attachments().Upload(
		t.Context(),
		"Form",
		"EntryID",
		"AttachField",
		"large.bin",
		bytes.NewReader([]byte("data")),
	)

	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, "File too large", apiErr.MessageText)
}

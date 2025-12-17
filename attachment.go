package remedy

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
)

// attachmentService implements AttachmentServicer for file operations.
type attachmentService struct {
	client *Client
}

// Get retrieves an attachment from an entry.
// The caller is responsible for closing the returned ReadCloser.
func (s *attachmentService) Get(ctx context.Context, form, entryID, fieldName string) (io.ReadCloser, error) {
	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return nil, err
	}
	defer s.client.queue.Release()

	path := attachmentPath(form, entryID, fieldName)

	req, cancel, err := s.client.newRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating attachment request: %w", err)
	}

	resp, err := s.client.do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("fetching attachment: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		// Parse error before closing body - parseAPIError reads from resp.Body
		apiErr := s.client.parseAPIError(resp)
		_ = resp.Body.Close()
		cancel()
		return nil, apiErr
	}

	// Return body for caller to read - they must close it
	return &attachmentReader{
		ReadCloser: resp.Body,
		cancel:     cancel,
	}, nil
}

// Upload uploads an attachment to an entry field.
func (s *attachmentService) Upload(ctx context.Context, form, entryID, fieldName, filename string, data io.Reader) error {
	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return err
	}
	defer s.client.queue.Release()

	// Create multipart form
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	// Write multipart in goroutine
	errCh := make(chan error, 1)
	go func() {
		defer func() {
			_ = pw.Close() // Always close pipe
		}()

		part, err := writer.CreateFormFile("entry", filename)
		if err != nil {
			errCh <- fmt.Errorf("creating form file: %w", err)
			return
		}

		if _, err := io.Copy(part, data); err != nil {
			errCh <- fmt.Errorf("copying data: %w", err)
			return
		}

		errCh <- writer.Close()
	}()

	path := attachmentPath(form, entryID, fieldName)

	req, cancel, err := s.client.newRequest(ctx, http.MethodPost, path, pr)
	if err != nil {
		return fmt.Errorf("creating upload request: %w", err)
	}
	defer cancel()

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := s.client.do(req)
	if err != nil {
		return wrapUploadError(err, errCh)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Wait for multipart writer to complete
	if writeErr := <-errCh; writeErr != nil {
		return writeErr
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return s.client.parseAPIError(resp)
	}

	return nil
}

// wrapUploadError wraps an HTTP error with any write error from the multipart goroutine.
func wrapUploadError(httpErr error, errCh <-chan error) error {
	// Drain the error channel to capture any write error
	// The goroutine will eventually complete due to pipe closure
	select {
	case writeErr := <-errCh:
		if writeErr != nil {
			return fmt.Errorf("uploading attachment: %w (write error: %w)", httpErr, writeErr)
		}
	default:
		// Goroutine hasn't written yet, just return the HTTP error
	}
	return fmt.Errorf("uploading attachment: %w", httpErr)
}

// attachmentPath builds the path for attachment operations.
func attachmentPath(form, entryID, fieldName string) string {
	return entryIDPath(form, entryID) + "/attach/" + url.PathEscape(fieldName)
}

// attachmentReader wraps an io.ReadCloser to also call cancel on close.
type attachmentReader struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *attachmentReader) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()

	return err
}

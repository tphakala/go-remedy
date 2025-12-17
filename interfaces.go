package remedy

import (
	"context"
	"io"
	"net/http"
)

// HTTPDoer abstracts the HTTP client for testing.
// *http.Client implements this interface.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// EntryServicer defines entry operations for the Remedy API.
// This interface enables mocking the entry service in tests.
type EntryServicer interface {
	// Get retrieves a single entry by ID.
	Get(ctx context.Context, form, entryID string, opts ...QueryOption) (*Entry, error)

	// List retrieves multiple entries with optional filtering and pagination.
	List(ctx context.Context, form string, opts ...QueryOption) (*EntryList, error)

	// Create creates a new entry in the specified form.
	Create(ctx context.Context, form string, values map[string]any) (*Entry, error)

	// Update updates an existing entry.
	Update(ctx context.Context, form, entryID string, values map[string]any) error

	// Delete removes an entry.
	Delete(ctx context.Context, form, entryID string, opts ...DeleteOption) error

	// Merge creates or updates an entry based on matching criteria.
	Merge(ctx context.Context, form string, values map[string]any) (*Entry, error)
}

// AttachmentServicer defines attachment operations for the Remedy API.
// This interface enables mocking the attachment service in tests.
type AttachmentServicer interface {
	// Get retrieves an attachment from an entry.
	Get(ctx context.Context, form, entryID, fieldName string) (io.ReadCloser, error)

	// Upload uploads an attachment to an entry.
	Upload(ctx context.Context, form, entryID, fieldName, filename string, data io.Reader) error
}

// RemedyClient defines the full client interface for the Remedy API.
// This interface enables mocking the entire client in consumer tests.
type RemedyClient interface {
	// Login authenticates with the Remedy server.
	Login(ctx context.Context, username, password string) error

	// LoginWithAuth authenticates with additional auth string.
	LoginWithAuth(ctx context.Context, username, password, authString string) error

	// Logout terminates the current session.
	Logout(ctx context.Context) error

	// Entries returns the entry service.
	Entries() EntryServicer

	// Attachments returns the attachment service.
	Attachments() AttachmentServicer
}

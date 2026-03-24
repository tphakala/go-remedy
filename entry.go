package remedy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// entryService implements EntryServicer for CRUD operations on form entries.
type entryService struct {
	client *Client
}

// Get retrieves a single entry by its ID.
func (s *entryService) Get(ctx context.Context, form, entryID string, opts ...QueryOption) (*Entry, error) {
	if form == "" {
		return nil, ErrEmptyFormName
	}
	if entryID == "" {
		return nil, ErrEmptyEntryID
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return nil, err
	}
	defer s.client.queue.Release()

	path := entryIDPath(form, entryID)
	params := buildQueryParams(opts)

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating get request: %w", err)
	}

	var entry Entry
	if err := s.client.doAndDecode(req, cancel, &entry); err != nil {
		return nil, fmt.Errorf("getting entry: %w", err)
	}

	return &entry, nil
}

// List retrieves multiple entries with optional filtering and pagination.
func (s *entryService) List(ctx context.Context, form string, opts ...QueryOption) (*EntryList, error) {
	if form == "" {
		return nil, ErrEmptyFormName
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return nil, err
	}
	defer s.client.queue.Release()

	path := entryPath(form)
	params := buildQueryParams(opts)

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("creating list request: %w", err)
	}

	var list EntryList
	if err := s.client.doAndDecode(req, cancel, &list); err != nil {
		return nil, fmt.Errorf("listing entries: %w", err)
	}

	return &list, nil
}

// Create creates a new entry in the specified form.
func (s *entryService) Create(ctx context.Context, form string, values map[string]any) (*Entry, error) {
	if form == "" {
		return nil, ErrEmptyFormName
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return nil, err
	}
	defer s.client.queue.Release()

	body := map[string]any{"values": values}

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodPost, entryPath(form), body)
	if err != nil {
		return nil, fmt.Errorf("creating create request: %w", err)
	}

	resp, err := s.client.do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("creating entry: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
		cancel()
	}()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, s.client.parseAPIError(resp)
	}

	var entry Entry

	// Remedy 25.2+ might return 201 Created with empty body but Location header
	if resp.StatusCode == http.StatusCreated && (resp.ContentLength == 0 || resp.Header.Get("Content-Length") == "0") {
		location := resp.Header.Get("Location")
		if location != "" {
			parts := strings.Split(location, "/")
			entryID := parts[len(parts)-1]
			entry.Values = map[string]any{"Entry_id": entryID}
			return &entry, nil
		}
	}

	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		// If it's 201 but decoding failed (maybe empty body), still try to get ID from location
		if resp.StatusCode == http.StatusCreated {
			location := resp.Header.Get("Location")
			if location != "" {
				parts := strings.Split(location, "/")
				entryID := parts[len(parts)-1]
				entry.Values = map[string]any{"Entry_id": entryID}
				return &entry, nil
			}
		}
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &entry, nil
}

// Update modifies an existing entry.
func (s *entryService) Update(ctx context.Context, form, entryID string, values map[string]any) error {
	if form == "" {
		return ErrEmptyFormName
	}
	if entryID == "" {
		return ErrEmptyEntryID
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return err
	}
	defer s.client.queue.Release()

	body := map[string]any{"values": values}

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodPut, entryIDPath(form, entryID), body)
	if err != nil {
		return fmt.Errorf("creating update request: %w", err)
	}

	if err := s.client.doAndDecode(req, cancel, nil); err != nil {
		return fmt.Errorf("updating entry: %w", err)
	}

	return nil
}

// Delete removes an entry.
func (s *entryService) Delete(ctx context.Context, form, entryID string, opts ...DeleteOption) error {
	if form == "" {
		return ErrEmptyFormName
	}
	if entryID == "" {
		return ErrEmptyEntryID
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return err
	}
	defer s.client.queue.Release()

	path := entryIDPath(form, entryID)

	if len(opts) > 0 {
		path += "?options=" + url.QueryEscape(string(opts[0]))
	}

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return fmt.Errorf("creating delete request: %w", err)
	}

	if err := s.client.doAndDecode(req, cancel, nil); err != nil {
		return fmt.Errorf("deleting entry: %w", err)
	}

	return nil
}

// Merge creates or updates an entry based on matching criteria.
func (s *entryService) Merge(ctx context.Context, form string, values map[string]any) (*Entry, error) {
	if form == "" {
		return nil, ErrEmptyFormName
	}

	if err := s.client.acquireAndRateLimit(ctx); err != nil {
		return nil, err
	}
	defer s.client.queue.Release()

	body := map[string]any{"values": values}
	path := apiBasePath + "/mergeEntry/" + url.PathEscape(form)

	req, cancel, err := s.client.newJSONRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("creating merge request: %w", err)
	}

	var entry Entry
	if err := s.client.doAndDecode(req, cancel, &entry); err != nil {
		return nil, fmt.Errorf("merging entry: %w", err)
	}

	return &entry, nil
}

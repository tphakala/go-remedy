package remedy

import (
	"errors"
	"fmt"
	"net/http"
)

// Sentinel errors for common API error conditions.
var (
	// ErrUnauthorized indicates authentication failure (HTTP 401).
	ErrUnauthorized = errors.New("remedy: unauthorized")

	// ErrForbidden indicates the user lacks permission (HTTP 403).
	ErrForbidden = errors.New("remedy: forbidden")

	// ErrNotFound indicates the requested resource does not exist (HTTP 404).
	ErrNotFound = errors.New("remedy: not found")

	// ErrNotAuthenticated indicates no valid token is available.
	ErrNotAuthenticated = errors.New("remedy: not authenticated")

	// ErrNoCredentials indicates credentials are not stored for automatic token refresh.
	ErrNoCredentials = errors.New("remedy: no credentials stored for token refresh")

	// ErrEmptyFormName indicates a form name parameter was empty.
	ErrEmptyFormName = errors.New("remedy: form name cannot be empty")

	// ErrEmptyEntryID indicates an entry ID parameter was empty.
	ErrEmptyEntryID = errors.New("remedy: entry ID cannot be empty")
)

// APIError represents an error returned by the BMC Remedy REST API.
// It contains the structured error information from the API response.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int

	// MessageType indicates the severity: ERROR, WARNING, FATAL, BAD STATUS.
	MessageType string

	// MessageText is the primary error description.
	MessageText string

	// MessageAppendedText provides additional context for the error.
	MessageAppendedText string

	// MessageNumber is the numeric error identifier.
	MessageNumber int
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.MessageAppendedText != "" {
		return fmt.Sprintf("remedy: %s (%d): %s - %s",
			e.MessageType, e.MessageNumber, e.MessageText, e.MessageAppendedText)
	}

	return fmt.Sprintf("remedy: %s (%d): %s",
		e.MessageType, e.MessageNumber, e.MessageText)
}

// Is implements errors.Is support for APIError.
// It allows checking against sentinel errors based on status code.
func (e *APIError) Is(target error) bool {
	switch {
	case errors.Is(target, ErrUnauthorized):
		return e.StatusCode == http.StatusUnauthorized
	case errors.Is(target, ErrForbidden):
		return e.StatusCode == http.StatusForbidden
	case errors.Is(target, ErrNotFound):
		return e.StatusCode == http.StatusNotFound
	default:
		return false
	}
}

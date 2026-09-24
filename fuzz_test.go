package remedy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzEscapeFieldName checks that an escaped field name cannot close the
// surrounding single quotes of a qualification, and that the escaping is
// reversible.
func FuzzEscapeFieldName(f *testing.F) {
	for _, seed := range []string{"Status", "O'Brien", "'", "''", "a' OR '1'='1", ""} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, field string) {
		escaped := escapeFieldName(field)

		// Every quote in the output is part of a doubled pair.
		assert.NotContains(t, strings.ReplaceAll(escaped, "''", ""), "'")
		assert.Equal(t, field, strings.ReplaceAll(escaped, "''", "'"))

		got := NewQuery().And(field, OpEqual, 1).Build()
		assert.Equal(t, "'"+escaped+"' = 1", got)
	})
}

// FuzzEntryFromLocation checks that a Location header of any shape either is
// rejected or yields a usable, single-segment entry ID.
func FuzzEntryFromLocation(f *testing.F) {
	for _, seed := range []string{
		"https://remedy.example.com/api/arsys/v1/entry/Form/000000000000001",
		"/api/arsys/v1/entry/Form/000000000000001/",
		"000000000000001",
		".",
		"/",
		"///",
		"",
		"%zz",
		"http://[::1",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, location string) {
		resp := &http.Response{Header: make(http.Header)}
		resp.Header.Set("Location", location)

		entry, ok := entryFromLocation(resp)
		if !ok {
			assert.Nil(t, entry)
			return
		}

		require.NotNil(t, entry)
		id, isString := entry.Values["Entry_id"].(string)
		require.True(t, isString, "Entry_id is %T, want string", entry.Values["Entry_id"])
		assert.NotEmpty(t, id)
		assert.NotEqual(t, ".", id)
		assert.NotContains(t, id, "/")
	})
}

// FuzzParseAPIError checks that any error body, well formed or not, becomes an
// *APIError that keeps the HTTP status and maps it to the matching sentinel.
func FuzzParseAPIError(f *testing.F) {
	f.Add(http.StatusNotFound, []byte(`[{"messageType":"ERROR","messageText":"Entry does not exist","messageNumber":302}]`))
	f.Add(http.StatusBadRequest, []byte(`[]`))
	f.Add(http.StatusUnauthorized, []byte(`not json`))
	f.Add(http.StatusForbidden, []byte(``))
	f.Add(http.StatusInternalServerError, []byte(`{"messageText":"object, not array"}`))

	f.Fuzz(func(t *testing.T, status int, body []byte) {
		// parseAPIError is only called for error statuses.
		status = http.StatusBadRequest + abs(status)%200

		resp := &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}

		err := parseAPIError(resp)
		apiErr, ok := errors.AsType[*APIError](err)
		require.True(t, ok, "parseAPIError returned %T, want *APIError", err)
		assert.Equal(t, status, apiErr.StatusCode)
		assert.Equal(t, status == http.StatusNotFound, errors.Is(err, ErrNotFound))
		assert.Equal(t, status == http.StatusUnauthorized, errors.Is(err, ErrUnauthorized))
		assert.Equal(t, status == http.StatusForbidden, errors.Is(err, ErrForbidden))
	})
}

// abs returns the absolute value of n, mapping math.MinInt to 0 so the result
// is never negative.
func abs(n int) int {
	if n < 0 {
		n = -n
	}
	return max(n, 0)
}

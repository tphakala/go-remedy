package remedy

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tphakala/go-remedy/internal/ratelimit"
)

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client for the Remedy client.
// This allows customizing transport, timeouts, and other HTTP settings.
// The client must implement the HTTPDoer interface (e.g., *http.Client).
func WithHTTPClient(httpClient HTTPDoer) Option {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithTimeout sets the timeout for individual API requests.
// The default is 30 seconds.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.timeout = d
	}
}

// WithRateLimit enables rate limiting with the specified requests per second.
// This helps prevent overwhelming the Remedy server.
func WithRateLimit(requestsPerSecond float64) Option {
	return func(c *Client) {
		c.rateLimiter = ratelimit.New(requestsPerSecond)
	}
}

// WithTokenLifetime sets how long tokens are considered valid.
// The default is 1 hour, matching BMC Remedy's standard token lifetime.
func WithTokenLifetime(d time.Duration) Option {
	return func(c *Client) {
		c.tokenLifetime = d
	}
}

// WithRefreshThreshold sets how long before expiry to refresh the token.
// The default is 5 minutes before expiry.
func WithRefreshThreshold(d time.Duration) Option {
	return func(c *Client) {
		c.refreshThreshold = d
	}
}

// WithAutoRefresh enables or disables automatic token refresh.
// When enabled (default), the client will automatically re-authenticate
// using stored credentials when the token is near expiry.
func WithAutoRefresh(enabled bool) Option {
	return func(c *Client) {
		c.autoRefresh = enabled
	}
}

// QueryOption configures entry query operations.
type QueryOption func(*queryOptions)

// queryOptions holds the configuration for query operations.
type queryOptions struct {
	fields       []string
	qualification string
	sortField    string
	sortOrder    SortOrder
	limit        int
	offset       int
	expand       []string
}

// WithFields specifies which fields to return in the response.
func WithFields(fields ...string) QueryOption {
	return func(o *queryOptions) {
		o.fields = fields
	}
}

// WithQualification sets the AR qualification string for filtering entries.
func WithQualification(q string) QueryOption {
	return func(o *queryOptions) {
		o.qualification = q
	}
}

// WithSort sets the field and order for sorting results.
func WithSort(field string, order SortOrder) QueryOption {
	return func(o *queryOptions) {
		o.sortField = field
		o.sortOrder = order
	}
}

// WithLimit sets the maximum number of entries to return.
func WithLimit(limit int) QueryOption {
	return func(o *queryOptions) {
		o.limit = limit
	}
}

// WithOffset sets the starting offset for pagination.
func WithOffset(offset int) QueryOption {
	return func(o *queryOptions) {
		o.offset = offset
	}
}

// WithExpand specifies associations to expand in the response.
func WithExpand(associations ...string) QueryOption {
	return func(o *queryOptions) {
		o.expand = associations
	}
}

// buildQueryParams converts query options to URL query parameters.
func buildQueryParams(opts []QueryOption) url.Values {
	o := &queryOptions{}
	for _, opt := range opts {
		opt(o)
	}

	params := url.Values{}

	if len(o.fields) > 0 {
		params.Set("fields", "values("+strings.Join(o.fields, ",")+")")
	}

	if o.qualification != "" {
		params.Set("q", o.qualification)
	}

	if o.sortField != "" {
		sortValue := o.sortField
		if o.sortOrder == SortDesc {
			sortValue += ".desc"
		}
		params.Set("sort", sortValue)
	}

	if o.limit > 0 {
		params.Set("limit", strconv.Itoa(o.limit))
	}

	if o.offset > 0 {
		params.Set("offset", strconv.Itoa(o.offset))
	}

	if len(o.expand) > 0 {
		params.Set("expand", strings.Join(o.expand, ","))
	}

	return params
}

# go-remedy

A native Go client library for the BMC Remedy AR System REST API.

[![Go Reference](https://pkg.go.dev/badge/github.com/tphakala/go-remedy.svg)](https://pkg.go.dev/github.com/tphakala/go-remedy)
[![Go Report Card](https://goreportcard.com/badge/github.com/tphakala/go-remedy)](https://goreportcard.com/report/github.com/tphakala/go-remedy)
[![CI](https://github.com/tphakala/go-remedy/actions/workflows/ci.yml/badge.svg)](https://github.com/tphakala/go-remedy/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/tphakala/go-remedy)](https://github.com/tphakala/go-remedy)

## Features

- JWT authentication with automatic token management
- Entry CRUD operations (Create, Read, Update, Delete, Merge)
- Attachment upload and download
- Type-safe query builder for AR qualifications
- Built-in request serialization (avoids BMC Error 9093)
- Token bucket rate limiting
- Context-aware with cancellation support
- Zero external runtime dependencies (stdlib only)

## Installation

```bash
go get github.com/tphakala/go-remedy
```

Requires Go 1.25 or later.

## Quick Start

```go
package main

import (
    "context"
    "log"
    "time"

    "github.com/tphakala/go-remedy"
)

func main() {
    // Create client with options
    client := remedy.New("https://remedy.example.com:8443",
        remedy.WithTimeout(30*time.Second),
        remedy.WithRateLimit(10), // 10 requests/second
    )
    defer client.Close()

    ctx := context.Background()

    // Authenticate
    if err := client.Login(ctx, "username", "password"); err != nil {
        log.Fatal(err)
    }
    defer client.Logout(ctx)

    // List entries with filtering
    entries, err := client.Entries().List(ctx, "HPD:Help Desk",
        remedy.WithQualification("'Status' = \"Open\""),
        remedy.WithFields("Request ID", "Summary", "Status"),
        remedy.WithLimit(100),
    )
    if err != nil {
        log.Fatal(err)
    }

    for _, entry := range entries.Entries {
        log.Printf("Request: %s - %s",
            entry.Values["Request ID"],
            entry.Values["Summary"])
    }
}
```

## Usage

### Client Configuration

```go
// Basic client
client := remedy.New("https://remedy.example.com:8443")

// With options
client := remedy.New("https://remedy.example.com:8443",
    remedy.WithHTTPClient(customHTTPClient),    // Custom HTTP client
    remedy.WithTimeout(60*time.Second),         // Request timeout
    remedy.WithRateLimit(5),                    // 5 requests/second
    remedy.WithTokenLifetime(time.Hour),        // JWT token lifetime (default: 1h)
    remedy.WithRefreshThreshold(5*time.Minute), // Refresh before expiry (default: 5m)
    remedy.WithAutoRefresh(true),               // Enable auto-refresh (default: true)
)
```

### Authentication

```go
// Standard login
err := client.Login(ctx, "username", "password")

// Login with additional auth string (for servers requiring extra context)
err := client.LoginWithAuth(ctx, "username", "password", "authString")

// Check authentication status
if client.IsAuthenticated() {
    // ...
}

// Logout
err := client.Logout(ctx)
```

### Automatic Token Refresh

BMC Remedy JWT tokens expire after 1 hour. The client automatically handles token refresh:

- **Credentials are stored** after `Login()` for automatic re-authentication
- **Proactive refresh** occurs when the token is within 5 minutes of expiry (configurable)
- **Concurrent safety** ensures only one refresh occurs even under high load

```go
// Tokens refresh automatically - no action needed for long-running applications
client := remedy.New("https://remedy.example.com:8443")
client.Login(ctx, "user", "pass")

// This works even hours later - token refreshes automatically
entries, _ := client.Entries().List(ctx, "HPD:Help Desk")

// For security-sensitive applications, clear stored credentials when done
client.ClearCredentials() // Disables auto-refresh, credentials removed from memory

// Disable auto-refresh entirely if preferred
client := remedy.New("https://remedy.example.com:8443",
    remedy.WithAutoRefresh(false),
)
```

### Entry Operations

```go
// Get single entry
entry, err := client.Entries().Get(ctx, "HPD:Help Desk", "REQ000001")

// List entries with options
entries, err := client.Entries().List(ctx, "HPD:Help Desk",
    remedy.WithQualification("'Status' = \"Open\""),
    remedy.WithFields("Request ID", "Summary", "Status"),
    remedy.WithSort("Create Date", remedy.SortDesc),
    remedy.WithLimit(50),
    remedy.WithOffset(100),
)

// Create entry
entry, err := client.Entries().Create(ctx, "HPD:Help Desk", map[string]any{
    "Summary":     "New ticket summary",
    "Description": "Detailed description",
    "Status":      "New",
})

// Update entry
err := client.Entries().Update(ctx, "HPD:Help Desk", "REQ000001", map[string]any{
    "Status": "In Progress",
})

// Delete entry
err := client.Entries().Delete(ctx, "HPD:Help Desk", "REQ000001")

// Delete with force option
err := client.Entries().Delete(ctx, "HPD:Help Desk", "REQ000001",
    remedy.DeleteOptionForce)

// Merge entry (create or update based on matching criteria)
entry, err := client.Entries().Merge(ctx, "HPD:Help Desk", map[string]any{
    "Summary": "Ticket summary",
    // Matching fields determine if create or update
})
```

### Query Builder

Build type-safe AR qualification strings:

```go
// Simple query
q := remedy.NewQuery().
    And("Status", "=", "Open").
    Build()
// Result: 'Status' = "Open"

// Complex query with multiple conditions
q := remedy.NewQuery().
    And("Status", "=", "Open").
    And("Priority", "<", 3).
    Or("Urgency", "=", "Critical").
    Build()
// Result: 'Status' = "Open" AND 'Priority' < 3 OR 'Urgency' = "Critical"

// Raw qualification for complex expressions
q := remedy.NewQuery().
    And("Status", "=", "Open").
    Raw("'Priority' < 3 OR 'Urgency' = \"High\"").
    Build()
// Result: 'Status' = "Open" AND ('Priority' < 3 OR 'Urgency' = "High")

// Use with List
entries, err := client.Entries().List(ctx, "Form",
    remedy.WithQualification(q),
)
```

Supported value types:
- Strings: `"value"` -> `"value"`
- Integers: `123` -> `123`
- Floats: `3.14` -> `3.14`
- Booleans: `true` -> `1`, `false` -> `0`
- Nil: `nil` -> `$NULL$`

### Attachments

```go
// Download attachment
reader, err := client.Attachments().Get(ctx, "Form", "EntryID", "FieldName")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

data, err := io.ReadAll(reader)

// Upload attachment
file, _ := os.Open("document.pdf")
defer file.Close()

err := client.Attachments().Upload(ctx, "Form", "EntryID", "FieldName",
    "document.pdf", file)
```

### Error Handling

```go
entry, err := client.Entries().Get(ctx, "Form", "InvalidID")
if err != nil {
    // Check for specific error types
    if errors.Is(err, remedy.ErrNotFound) {
        log.Println("Entry not found")
    } else if errors.Is(err, remedy.ErrUnauthorized) {
        log.Println("Authentication required")
    } else if errors.Is(err, remedy.ErrForbidden) {
        log.Println("Permission denied")
    } else if errors.Is(err, remedy.ErrNoCredentials) {
        log.Println("Token expired and no credentials for refresh")
    }

    // Get detailed API error
    var apiErr *remedy.APIError
    if errors.As(err, &apiErr) {
        log.Printf("API Error %d: %s - %s",
            apiErr.MessageNumber,
            apiErr.MessageText,
            apiErr.MessageAppendedText)
    }
}
```

## Request Serialization

BMC Remedy enforces per-user session limits. Concurrent requests with the same user account trigger Error 9093: "User is currently connected from another machine or incompatible session".

This library automatically serializes requests to prevent this error. All API calls pass through an internal queue ensuring only one request executes at a time per client.

## Testing

The library provides interfaces for all services, making it easy to mock in tests:

```go
// Your code uses interfaces
type EntryServicer interface {
    Get(ctx context.Context, form, entryID string, opts ...QueryOption) (*Entry, error)
    List(ctx context.Context, form string, opts ...QueryOption) (*EntryList, error)
    Create(ctx context.Context, form string, values map[string]any) (*Entry, error)
    Update(ctx context.Context, form, entryID string, values map[string]any) error
    Delete(ctx context.Context, form, entryID string, opts ...DeleteOption) error
    Merge(ctx context.Context, form string, values map[string]any) (*Entry, error)
}
```

Use [mockery](https://github.com/vektra/mockery) to generate mocks:

```bash
mockery --all --dir=. --output=mocks --outpkg=mocks
```

## License

MIT License - see [LICENSE](LICENSE) for details.

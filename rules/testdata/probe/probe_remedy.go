package probe

// Probes for the go-remedy matchers in rules/remedy.go. The matchers are gated
// on the package path, and this probe package is inside the allowed set.
// client.go and probe_remedy_test.go in this directory hold the negative cases
// for the file-name exclusions.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/tphakala/go-remedy/internal/queue"
)

// remedyClient mirrors the remedy.Client fields the matchers look at.
type remedyClient struct {
	httpClient *http.Client
	queue      *queue.Queue
	token      string
}

// rule: RemedyQueueAcquire
func remedyQueueAcquire(ctx context.Context, c *remedyClient, q *queue.Queue) {
	_ = c.queue.Acquire(ctx) // want "take the request queue through acquireAndRateLimit"
	_ = q.Acquire(ctx)       // want "take the request queue through acquireAndRateLimit"
	// Not flagged: the matcher covers Acquire only.
	c.queue.Release()
}

// rule: RemedyDirectHTTPDo
func remedyDirectHTTPDo(c *remedyClient, req *http.Request) (*http.Response, error) {
	return c.httpClient.Do(req) // want "send requests through c.do(req)"
}

// rule: RemedyUnboundedBodyRead
func remedyUnboundedBodyRead(resp *http.Response) {
	_, _ = io.ReadAll(resp.Body) // want "wrap resp.Body in io.LimitReader"
	// Not flagged: the read is bounded.
	_, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<16))
}

// rule: RemedySecretInFormat
func remedySecretInFormat(c *remedyClient, username, password, authString string) {
	_ = fmt.Sprintf("login %s", password)                                   // want "password looks like a credential or token"
	_ = fmt.Errorf("auth %s failed: %w", authString, errors.ErrUnsupported) // want "authString looks like a credential or token"
	_ = fmt.Sprint("t=", c.token)                                           // want "c.token looks like a credential or token"
	_ = errors.New(c.token)                                                 // want "c.token looks like a credential or token"
	// Not flagged: not a secret by name.
	_ = fmt.Sprintf("user %s", username)
}

const (
	probeAPIBase          = "/api/arsys/v1"
	probeForm             = "HPD:Help Desk"
	probeTypedForm string = "HPD:Help Desk"
)

// rule: RemedyUnescapedPathSegment
func remedyUnescapedPathSegment(form, entryID string) {
	_ = probeAPIBase + "/entry/" + form           // want "escape form with url.PathEscape"
	_ = "/entry/" + entryID                       // want "escape entryID with url.PathEscape"
	_ = probeAPIBase + "/entry/" + probeForm      // want "escape probeForm with url.PathEscape"
	_ = probeAPIBase + "/entry/" + probeTypedForm // want "escape probeTypedForm with url.PathEscape"
	// Not flagged: escaped, a literal path piece, or not after a path
	// separator.
	_ = probeAPIBase + "/entry/" + url.PathEscape(form)
	_ = probeAPIBase + "/entry/" + "login"
	_ = probeAPIBase + "/entry/" + `login`
	_ = "values(" + form + ")"
}

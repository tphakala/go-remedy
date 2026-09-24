//go:build ruleguard

package gorules

import "github.com/quasilyte/go-ruleguard/dsl"

// Project-specific matchers for go-remedy. Each one enforces an architecture
// invariant from AGENTS.md. They are gated with m.File().PkgPath to the root
// package plus the probe package that exercises them, and they skip _test.go
// files, where mocks and fixtures break these rules on purpose.

// RemedyQueueAcquire flags a direct Acquire on the request queue outside
// client.go and auth.go.
//
// Every API call must go through c.acquireAndRateLimit(ctx), which refreshes
// the token first and applies the rate limiter after taking the queue. A
// service method that calls c.queue.Acquire itself skips both. Login uses
// loginAcquireQueue in auth.go, which is the one deliberate exception.
func RemedyQueueAcquire(m dsl.Matcher) {
	m.Import("github.com/tphakala/go-remedy/internal/queue")
	m.Match(`$q.Acquire($ctx)`).
		Where(m["q"].Type.Is("*queue.Queue") &&
			m.File().PkgPath.Matches(`^github\.com/tphakala/go-remedy(/rules/testdata/probe)?$`) &&
			!m.File().Name.Matches(`^(client|auth)\.go$|_test\.go$`)).
		Report("take the request queue through acquireAndRateLimit(ctx), which also refreshes the token and applies the rate limit; call queue.Acquire directly only in client.go or auth.go")
}

// RemedyDirectHTTPDo flags a call to the httpClient field's Do method outside
// client.go.
//
// Requests go through (*Client).do so transport errors are wrapped the same way
// on every path.
func RemedyDirectHTTPDo(m dsl.Matcher) {
	m.Match(`$c.httpClient.Do($req)`).
		Where(m.File().PkgPath.Matches(`^github\.com/tphakala/go-remedy(/rules/testdata/probe)?$`) &&
			!m.File().Name.Matches(`^client\.go$|_test\.go$`)).
		Report("send requests through c.do(req) instead of calling httpClient.Do directly")
}

// RemedyUnboundedBodyRead flags io.ReadAll on an HTTP response body.
//
// A misbehaving or hostile server can return an arbitrarily large body. Wrap
// the body in io.LimitReader first, as the login path does with maxTokenSize.
func RemedyUnboundedBodyRead(m dsl.Matcher) {
	m.Match(`io.ReadAll($resp.Body)`).
		Where(m["resp"].Type.Is("*http.Response") &&
			m.File().PkgPath.Matches(`^github\.com/tphakala/go-remedy(/rules/testdata/probe)?$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report("wrap $resp.Body in io.LimitReader before io.ReadAll; never read a server response without a size bound")
}

// RemedySecretInFormat flags a credential or token passed to a fmt function or
// errors.New.
//
// The match is by expression text (an identifier or selector ending in
// password, authString, token, creds or credentials), not by data flow, so a
// secret copied into a differently named variable is not caught, and neither is
// a secret passed after the fourth argument of a fmt call. It exists to
// stop the obvious slip of formatting a secret into an error message.
func RemedySecretInFormat(m dsl.Matcher) {
	// One pattern per argument position: `fmt.$_($*_, $x, $*_)` matched
	// nothing (MEASURED against golangci-lint 2.13.2).
	m.Match(
		`fmt.$_($x, $*_)`,
		`fmt.$_($_, $x, $*_)`,
		`fmt.$_($_, $_, $x, $*_)`,
		`fmt.$_($_, $_, $_, $x, $*_)`,
		`errors.New($x)`,
	).
		Where(m["x"].Text.Matches(`(^|\.)(password|authString|token|creds|credentials)$`) &&
			m.File().PkgPath.Matches(`^github\.com/tphakala/go-remedy(/rules/testdata/probe)?$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report("$x looks like a credential or token; never format it into a string or error")
}

// RemedyUnescapedPathSegment flags a URL path built by appending a string
// expression after a literal ending in "/", unless that expression is itself a
// string literal or a url.PathEscape call.
//
// Form names, entry IDs and field names routinely contain spaces, colons and
// slashes ("HPD:Help Desk"). Unescaped, they change which endpoint the request
// reaches. A named constant is flagged too, since a constant form name needs
// the same escaping; only a literal is taken as a deliberate path piece.
func RemedyUnescapedPathSegment(m dsl.Matcher) {
	m.Match(`$a + $sep + $x`, `$sep + $x`).
		Where(m["sep"].Text.Matches("^\".*/\"$") &&
			!m["x"].Text.Matches("^[\"`]") &&
			!m["x"].Text.Matches(`^url\.PathEscape\(`) &&
			m.File().PkgPath.Matches(`^github\.com/tphakala/go-remedy(/rules/testdata/probe)?$`) &&
			!m.File().Name.Matches(`_test\.go$`)).
		Report("escape $x with url.PathEscape before appending it to a URL path")
}

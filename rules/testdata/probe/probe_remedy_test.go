package probe

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// Negative probe: the go-remedy matchers skip _test.go files, where mocks and
// fixtures break the production rules on purpose, so nothing in this file may
// produce a finding.
func remedyTestFilesAreExempt(ctx context.Context, c *remedyClient, req *http.Request, password string) {
	_ = c.queue.Acquire(ctx)
	resp, _ := c.httpClient.Do(req)
	_, _ = io.ReadAll(resp.Body)
	_ = fmt.Sprintf("login %s", password)
	_ = "/entry/" + password
}

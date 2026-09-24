package probe

import (
	"context"
	"net/http"
)

// Negative probe: RemedyQueueAcquire and RemedyDirectHTTPDo allow client.go,
// so nothing in this file may produce a finding.
func clientGoIsExempt(ctx context.Context, c *remedyClient, req *http.Request) (*http.Response, error) {
	if err := c.queue.Acquire(ctx); err != nil {
		return nil, err
	}
	defer c.queue.Release()
	return c.httpClient.Do(req)
}

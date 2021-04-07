package refreshabletransport

import (
	"context"
	"net/http"
	"time"

	"github.com/palantir/pkg/refreshable"
)

type RefreshableHTTPClient interface {
	refreshable.Refreshable
	CurrentHTTPClient() *http.Client
}

type refreshableHTTPClient struct {
	refreshable.Refreshable
}

func (r refreshableHTTPClient) CurrentHTTPClient() *http.Client {
	return r.Current().(*http.Client)
}

func NewRefreshableHTTPClient(ctx context.Context, rt http.RoundTripper, timeout refreshable.Duration) RefreshableHTTPClient {
	return refreshableHTTPClient{
		Refreshable: timeout.MapDuration(func(timeout time.Duration) interface{} {
			return &http.Client{
				Timeout:   timeout,
				Transport: rt,
			}
		}),
	}
}

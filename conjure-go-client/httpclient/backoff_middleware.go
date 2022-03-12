package httpclient

import (
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/retry"
	"net/http"
	"net/url"
)

type backoffMiddleware struct {
	retrier       retry.Retrier
	attemptedURIs map[string]struct{}
	backoffFunc   func()
}

// NewBackoffMiddleware returns middleware that uses a supplied Retrier to backoff before making requests if the client
// has attempted to reach the URI before or has sent too many requests.
func NewBackoffMiddleware(retrier retry.Retrier) Middleware {
	return &backoffMiddleware{
		retrier:       retrier,
		attemptedURIs: map[string]struct{}{},
	}
}

func (b *backoffMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	b.backoffRequest(req)
	resp, err := next.RoundTrip(req)
	b.handleResponse(resp)
	return resp, err
}

func (b *backoffMiddleware) backoffRequest(req *http.Request) {
	baseURI := getBaseURI(req.URL)
	defer func() {
		b.attemptedURIs[baseURI] = struct{}{}
	}()
	// Use backoffFunc if backoff behavior was determined by previous response e.g. throttle on 429
	if b.backoffFunc != nil {
		b.backoffFunc()
		b.backoffFunc = nil
		return
	}
	// Trigger retrier on first attempt so that future attempts have backoff
	if len(b.attemptedURIs) == 0 {
		b.retrier.Next()
	}
	// Trigger retrier for backoff if URI was attempted before
	if _, performBackoff := b.attemptedURIs[baseURI]; performBackoff {
		b.retrier.Next()
	}
}

func (b *backoffMiddleware) handleResponse(resp *http.Response) {
	if resp != nil {
		switch resp.StatusCode {
		case internal.StatusCodeRetryOther, internal.StatusCodeRetryTemporaryRedirect:
			b.retrier.Reset()
		case internal.StatusCodeThrottle:
			b.backoffFunc = func() { b.retrier.Next() }
		}
	}
}

func getBaseURI(u *url.URL) string {
	uCopy := url.URL{
		Scheme: u.Scheme,
		Opaque: u.Opaque,
		User:   u.User,
		Host:   u.Host,
	}
	return uCopy.String()
}

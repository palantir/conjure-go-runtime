// Copyright (c) 2018 Palantir Technologies. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package httpclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
)

// A Client executes requests to a configured service.
//
// The Get/Head/Post/Put/Delete methods are for conveniently setting the method type and calling Do()
type Client interface {
	// Do executes a full request. Any input or output should be specified via params.
	// By the time it is returned, the response's body will be fully read and closed.
	// Use the WithResponse* params to unmarshal the body before Do() returns.
	//
	// In the case of a response with StatusCode >= 400, Do() will return a nil response and a non-nil error.
	// Use StatusCodeFromError(err) to retrieve the code from the error.
	// Use WithDisableRestErrors() to disable this middleware on your client.
	// Use WithErrorDecoder(errorDecoder) to replace this default behavior with custom error decoding behavior.
	Do(ctx context.Context, params ...RequestParam) (*http.Response, error)

	Get(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Head(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Post(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Put(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Delete(ctx context.Context, params ...RequestParam) (*http.Response, error)
}

type clientImpl struct {
	client                 RefreshableHTTPClient
	middlewares            []Middleware
	errorDecoderMiddleware Middleware
	recoveryMiddleware     Middleware

	uriScorer      internal.RefreshableURIScoringMiddleware
	maxAttempts    refreshable.IntPtr // 0 means no limit. If nil, uses 2*len(uris).
	backoffOptions refreshingclient.RefreshableRetryParams
	bufferPool     bytesbuffers.Pool
}

func (c *clientImpl) Get(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodGet))...)
}

func (c *clientImpl) Head(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodHead))...)
}

func (c *clientImpl) Post(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodPost))...)
}

func (c *clientImpl) Put(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodPut))...)
}

func (c *clientImpl) Delete(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	return c.Do(ctx, append(params, WithRequestMethod(http.MethodDelete))...)
}

func (c *clientImpl) Do(ctx context.Context, params ...RequestParam) (*http.Response, error) {
	uris := c.uriScorer.CurrentURIScoringMiddleware().GetURIsInOrderOfIncreasingScore()
	if len(uris) == 0 {
		return nil, werror.ErrorWithContextParams(ctx, "no base URIs are configured")
	}

	maxAttempts := 2 * len(uris)
	if c.maxAttempts != nil {
		if confMaxAttempts := c.maxAttempts.CurrentIntPtr(); confMaxAttempts != nil {
			maxAttempts = *confMaxAttempts
		}
	}
	b, err := applyRequestParams(c.bufferPool, params...)
	if err != nil {
		return nil, err
	}
	for _, c := range b.configureCtx {
		ctx = c(ctx)
	}
	req, err := getRequest(ctx, b)
	if err != nil {
		return nil, err
	}
	cancelled := false
	cancelFunc := func() { cancelled = true }
	retrier := c.backoffOptions.CurrentRetryParams().Start(ctx)
	clientCopy := c.getClientCopyWithMiddleware(b.errorDecoderMiddleware, b.bodyMiddleware, uris, retrier, cancelFunc)
	attempts := 0
	var resp *http.Response
	for !cancelled && (maxAttempts == 0 || attempts < maxAttempts) {
		reqCopy := req.Clone(ctx)
		resp, err = clientCopy.Do(reqCopy)
		err = unwrapURLError(ctx, err)
		// unless this is exactly the scenario where the caller has opted into being responsible for draining and closing
		// the response body, be sure to do so here.
		if !b.rawOutput {
			internal.DrainBody(resp)
		}
		attempts++
		if resp != nil && isSuccessfulOrBadRequest(resp.StatusCode) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	return resp, err
}

func isSuccessfulOrBadRequest(statusCode int) bool {
	return statusCode < 300 || (statusCode >= http.StatusBadRequest && statusCode < http.StatusInternalServerError)
}

func applyRequestParams(bufferPool bytesbuffers.Pool, params ...RequestParam) (*requestBuilder, error) {
	b := &requestBuilder{
		headers:        make(http.Header),
		query:          make(url.Values),
		bodyMiddleware: &bodyMiddleware{bufferPool: bufferPool},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	return b, nil
}

func getRequest(ctx context.Context, b *requestBuilder) (*http.Request, error) {
	if b.method == "" {
		return nil, werror.ErrorWithContextParams(ctx, "httpclient: use WithRequestMethod() to specify HTTP method")
	}
	req, err := http.NewRequestWithContext(ctx, b.method, b.path, nil)
	if err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to build new HTTP request")
	}
	req.Header = b.headers
	if q := b.query.Encode(); q != "" {
		req.URL.RawQuery = q
	}
	return req, nil
}

func (c *clientImpl) getClientCopyWithMiddleware(errorDecoderMiddleware Middleware, bodyMiddleware *bodyMiddleware, uris []string, backoffRetrier retry.Retrier, cancelFunc func()) http.Client {
	// shallow copy so we can overwrite the Transport with a wrapped one.
	clientCopy := *c.client.CurrentHTTPClient()
	transport := clientCopy.Transport // start with the client's transport configured with default middleware

	// must precede the error decoders to read the status code of the raw response.
	transport = wrapTransport(transport, c.uriScorer.CurrentURIScoringMiddleware())
	// request decoder must precede the client decoder
	// must precede the body middleware to read the response body
	transport = wrapTransport(transport, errorDecoderMiddleware, c.errorDecoderMiddleware)
	// must precede the body middleware to read the request body
	transport = wrapTransport(transport, c.middlewares...)
	// must wrap inner middlewares to mutate the return values
	transport = wrapTransport(transport, bodyMiddleware)
	// must precede URI middleware to track attempted URIs
	transport = wrapTransport(transport, NewBackoffMiddleware(backoffRetrier))
	// must wrap inner middlewares to update request with resolved URL
	transport = wrapTransport(transport, NewURIMiddleware(uris, cancelFunc))
	// must be the outermost middleware to recover panics in the rest of the request flow
	// there is a second, inner recoveryMiddleware in the client's default middlewares so that panics
	// inside the inner-most RoundTrip benefit from traceIDs and loggers set on the context.
	transport = wrapTransport(transport, c.recoveryMiddleware)

	clientCopy.Transport = transport
	return clientCopy
}

// unwrapURLError converts a *url.Error to a werror. We need this because all
// errors from the stdlib's client.Do are wrapped in *url.Error, and if we
// were to blindly return that we would lose any werror params stored on the
// underlying Err.
func unwrapURLError(ctx context.Context, respErr error) error {
	if respErr == nil {
		return nil
	}

	urlErr, ok := respErr.(*url.Error)
	if !ok {
		// We don't recognize this as a url.Error, just return the original.
		return respErr
	}
	return urlErr.Err
}

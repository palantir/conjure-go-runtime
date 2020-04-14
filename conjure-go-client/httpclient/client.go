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
	"math/rand"
	"net/http"
	"net/url"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-tracing/wtracing"
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
	client                 http.Client
	middlewares            []Middleware
	errorDecoderMiddleware Middleware
	metricsMiddleware      Middleware

	uris                          []string
	maxRetries                    int
	disableTraceHeaderPropagation bool
	backoffOptions                []retry.Option
	bufferPool                    bytesbuffers.Pool
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
	uris := c.uris
	if len(uris) == 0 {
		return nil, werror.Error("no base URIs are configured")
	}
	offset := rand.Intn(len(uris))

	var err error
	var resp *http.Response

	retrier := retry.Start(ctx, c.backoffOptions...)

	nextURI := uris[offset]
	failedURIs := map[string]struct{}{}
	for i := 0; i < c.maxRetries; i++ {
		resp, err = c.doOnce(ctx, nextURI, params...)
		if retryOther, _ := internal.IsThrottleResponse(resp, err); retryOther {
			// 429: throttle
			// Immediately backoff and select the next URI.
			// TODO(whickman): use the retry-after header once #81 is resolved
			nextURI, offset = nextURIAndBackoff(nextURI, uris, offset, failedURIs, retrier)
		} else if internal.IsUnavailableResponse(resp, err) {
			// 503: go to next node
			nextURI, offset = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
		} else if resp == nil {
			// If we get a nil response, we can assume there is a problem with host and can move on to the next.
			nextURI, offset = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
		} else if shouldTryOther, otherURI := internal.IsRetryOtherResponse(resp); shouldTryOther {
			// 308: go to next node, or particular node if provided.
			if otherURI != nil {
				nextURI = otherURI.String()
				retrier.Reset()
			} else {
				nextURI, offset = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
			}
		} else {
			// The response was not a failure in any way, return the error
			return resp, err
		}
	}
	if err != nil {
		return resp, err
	}
	return resp, werror.Error("could not find live server")
}

// If lastURI was already marked failed, we perform a backoff as determined by the retrier before returning the next URI and its offset.
// Otherwise, we add lastURI to failedURIs and return the next URI and its offset immediately.
func nextURIOrBackoff(lastURI string, uris []string, offset int, failedURIs map[string]struct{}, retrier retry.Retrier) (nextURI string, nextURIOffset int) {
	_, performBackoff := failedURIs[lastURI]
	nextURI, nextURIOffset = markFailedAndGetNextURI(failedURIs, lastURI, offset, uris)
	// If the URI has failed before, perform a backoff
	if performBackoff {
		retrier.Next()
	}
	return nextURI, nextURIOffset
}

// Marks the current URI as failed, gets the next URI, and performs a backoff as determined by the retrier.
func nextURIAndBackoff(lastURI string, uris []string, offset int, failedURIs map[string]struct{}, retrier retry.Retrier) (nextURI string, nextURIOffset int) {
	nextURI, nextURIOffset = markFailedAndGetNextURI(failedURIs, lastURI, offset, uris)
	retrier.Next()
	return nextURI, nextURIOffset

}

func markFailedAndGetNextURI(failedURIs map[string]struct{}, lastURI string, offset int, uris []string) (string, int) {
	failedURIs[lastURI] = struct{}{}
	nextURIOffset := (offset + 1) % len(uris)
	nextURI := uris[nextURIOffset]
	return nextURI, nextURIOffset
}

func (c *clientImpl) doOnce(ctx context.Context, baseURI string, params ...RequestParam) (*http.Response, error) {

	// 1. create the request
	b := &requestBuilder{
		headers:        c.initializeRequestHeaders(ctx),
		query:          make(url.Values),
		bodyMiddleware: &bodyMiddleware{bufferPool: c.bufferPool},
	}

	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	for _, c := range b.configureCtx {
		ctx = c(ctx)
	}

	if b.method == "" {
		return nil, werror.Error("httpclient: use WithRequestMethod() to specify HTTP method")
	}

	req, err := http.NewRequest(b.method, baseURI+b.path, nil)
	if err != nil {
		return nil, werror.Wrap(err, "failed to build new HTTP request")
	}
	req = req.WithContext(ctx)
	req.Header = b.headers
	if q := b.query.Encode(); q != "" {
		req.URL.RawQuery = q
	}

	// 2. create the transport and client
	// shallow copy so we can overwrite the Transport with a wrapped one.
	clientCopy := c.client
	transport := clientCopy.Transport // start with the concrete http.Transport from the client

	middlewares := []Middleware{
		// must precede the error decoders because they return a nil response and the metrics need the status code of
		// the raw response.
		c.metricsMiddleware,
		// must precede the client error decoder
		b.errorDecoderMiddleware,
		// must precede the body middleware so it can read the response body
		c.errorDecoderMiddleware,
	}
	// must precede the body middleware so it can read the request body
	middlewares = append(middlewares, c.middlewares...)
	middlewares = append(middlewares, b.bodyMiddleware)
	for _, middleware := range middlewares {
		if middleware != nil {
			transport = wrapTransport(transport, middleware)
		}
	}
	clientCopy.Transport = transport

	// 3. execute the request using the client to get and handle the response
	resp, respErr := clientCopy.Do(req)

	// unless this is exactly the scenario where the caller has opted into being responsible for draining and closing
	// the response body, be sure to do so here.
	if !(respErr == nil && b.bodyMiddleware.rawOutput) {
		internal.DrainBody(resp)
	}

	return resp, unwrapURLError(respErr)
}

// unwrapURLError converts a *url.Error to a werror. We need this because all
// errors from the stdlib's client.Do are wrapped in *url.Error, and if we
// were to blindly return that we would lose any werror params stored on the
// underlying Err.
func unwrapURLError(respErr error) error {
	if respErr == nil {
		return nil
	}

	urlErr, ok := respErr.(*url.Error)
	if !ok {
		// We don't recognize this as a url.Error, just return the original.
		return respErr
	}
	params := []werror.Param{werror.SafeParam("requestMethod", urlErr.Op)}

	if parsedURL, _ := url.Parse(urlErr.URL); parsedURL != nil {
		params = append(params,
			werror.SafeParam("requestHost", parsedURL.Host),
			werror.UnsafeParam("requestPath", parsedURL.Path))
	}

	return werror.Wrap(urlErr.Err, "httpclient request failed", params...)
}

func (c *clientImpl) initializeRequestHeaders(ctx context.Context) http.Header {
	headers := make(http.Header)
	if !c.disableTraceHeaderPropagation {
		traceID := wtracing.TraceIDFromContext(ctx)
		if traceID != "" {
			headers.Set(traceIDHeaderKey, string(traceID))
		}
	}
	return headers
}

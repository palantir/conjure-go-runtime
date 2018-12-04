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

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/witchcraft-go-error"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
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
	// Use StatusCodeFromError(err) to retrieve the code from the error
	// and WithDisableRestErrors() to disable this middleware on your client.
	Do(ctx context.Context, params ...RequestParam) (*http.Response, error)

	Get(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Head(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Post(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Put(ctx context.Context, params ...RequestParam) (*http.Response, error)
	Delete(ctx context.Context, params ...RequestParam) (*http.Response, error)
}

type clientImpl struct {
	client      http.Client
	middlewares []Middleware

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
	offset := rand.Intn(len(uris))

	var err error
	var resp *http.Response

	retrier := retry.Start(ctx, c.backoffOptions...)

	nextURI := uris[offset]
	failedURIs := map[string]struct{}{}
	for i := 0; i < c.maxRetries; i++ {
		resp, err = c.doOnce(ctx, nextURI, params...)
		if resp == nil {
			// If we get a nil response, we can assume there is a problem with host and can move on to the next.
			nextURI = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
		} else if shouldThrottle, _ := internal.IsThrottleResponse(resp); shouldThrottle {
			// 429: throttle
			// Ideally we should avoid hitting this URI until it's next available. In the interest of avoiding
			// complex state in the client that will be replaced with a service-mesh, we will simply move on to the next
			// available URI
			nextURI = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
		} else if shouldTryOther, otherURI := internal.IsRetryOtherResponse(resp); shouldTryOther {
			// 308: go to next node, or particular node if provided.
			if otherURI != nil {
				nextURI = otherURI.String()
				retrier.Reset()
			} else {
				nextURI = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
			}
		} else if internal.IsUnavailableResponse(resp) {
			// 503: go to next node
			nextURI = nextURIOrBackoff(nextURI, uris, offset, failedURIs, retrier)
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

// If lastURI was already marked failed, we perform a backoff as determined by the retrier.
// Otherwise, we add lastURI to failedURIs and return the next URI immediately.
func nextURIOrBackoff(lastURI string, uris []string, offset int, failedURIs map[string]struct{}, retrier retry.Retrier) string {
	_, performBackoff := failedURIs[lastURI]
	failedURIs[lastURI] = struct{}{}
	nextURI := uris[(offset+1)%len(uris)]
	// If the URI has failed before, perform a backoff
	if performBackoff {
		retrier.Next()
	}
	return nextURI
}

func (c *clientImpl) doOnce(ctx context.Context, baseURI string, params ...RequestParam) (*http.Response, error) {
	req, reqMiddlewares, err := c.newRequest(ctx, baseURI, params...)
	if err != nil {
		return nil, err
	}

	// shallow copy so we can overwrite the Transport with a wrapped one.
	clientCopy := c.client
	transport := clientCopy.Transport

	for _, middleware := range reqMiddlewares {
		m := middleware
		transport = wrapTransport(transport, m)
	}
	clientCopy.Transport = transport

	resp, respErr := clientCopy.Do(req)

	return resp, unwrapURLError(respErr)
}

// unwrapURLError converts a *url.Error to a werror. We need this because all
// errors from the stdlib's client.Do are wrapped in *url.Error, and if we
// were to blindly return that we would lose any zerror params stored on the
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

// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc

import (
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// fluentClient implements Client by wrapping a standard *http.Client with
// retry and URI scoring logic.
type fluentClient struct {
	serviceName    refreshable.Refreshable[string]
	httpClient     refreshable.Refreshable[*http.Client]
	middlewares    []Middleware
	errorDecoder   ErrorDecoder // client-level error decoder
	recoveryMW     Middleware
	uriScorer      internal.URIScoringMiddleware
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	bufferPool     bytesbuffers.Pool
}

type configurableClient[B ServiceBuilder[B]] struct {
	fluentClient
	// retains a reference to the builder for ConfigurableClient.Builder().
	builder B
}

func (c *configurableClient[B]) Builder() B {
	return c.builder.Clone()
}

// getBufferPool returns the client's buffer pool, implementing poolProvider.
func (c *fluentClient) getBufferPool() bytesbuffers.Pool {
	return c.bufferPool
}

// errorDecoderProvider is implemented by clients that carry a client-level ErrorDecoder.
// Endpoint.Execute uses this to extract the decoder before wrapping with per-endpoint middleware.
type errorDecoderProvider interface {
	getErrorDecoder() ErrorDecoder
}

func (c *fluentClient) getErrorDecoder() ErrorDecoder {
	return c.errorDecoder
}

func (c *fluentClient) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	uris := c.uriScorer.GetURIsInOrderOfIncreasingScore()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "", werror.SafeParam("serviceName", c.serviceName.Current()))
	}

	attempts := 2 * len(uris)
	if c.maxAttempts != nil {
		if confMaxAttempts := c.maxAttempts.Current(); confMaxAttempts != nil {
			attempts = *confMaxAttempts
		}
	}

	backoff := retry.Start(ctx, retry.WithInitialBackoff(c.initialBackoff.Current()), retry.WithMaxBackoff(c.maxBackoff.Current()))
	retrier := internal.NewRequestRetrier(uris, backoff, attempts)
	uri, isRelocated := retrier.GetNextURI(nil, nil)
	for {
		resp, retryable, err := c.doOnce(req, uri, isRelocated)
		if !retryable {
			return resp, err
		}
		uri, isRelocated = retrier.GetNextURI(resp, err)
		if uri == "" {
			return resp, err
		}
		// Drain and close the retried response body to free resources.
		drainBody(ctx, resp)
		if err != nil {
			svc1log.FromContext(ctx).Debug("Retrying request", svc1log.Stacktrace(err))
		} else if resp != nil {
			svc1log.FromContext(ctx).Debug("Retrying request",
				svc1log.SafeParam("statusCode", resp.StatusCode))
		}
	}
}

func (c *fluentClient) doOnce(
	origReq *http.Request,
	baseURI string,
	useBaseURIOnly bool,
) (_ *http.Response, retryable bool, _ error) {
	ctx := origReq.Context()

	// Clone the request for this attempt.
	req := origReq.Clone(ctx)

	// Construct the full URL by joining the base URI with the request's path.
	baseURL, err := url.Parse(baseURI)
	if err != nil {
		return nil, false, werror.WrapWithContextParams(ctx, err, "invalid URL")
	}
	if useBaseURIOnly {
		req.URL = baseURL
	} else {
		// Use JoinPath().String() and re-parse to produce a correct url.URL.
		// JoinPath on a URL with an empty path (e.g., http://host) returns a URL
		// whose Path field lacks a leading slash, which results in an invalid
		// HTTP/1.1 request-target. Re-parsing the string representation fixes this.
		joinedURL, err := url.Parse(baseURL.JoinPath(origReq.URL.Path).String())
		if err != nil {
			return nil, false, werror.WrapWithContextParams(ctx, err, "failed to construct request URL")
		}
		joinedURL.RawQuery = origReq.URL.RawQuery
		req.URL = joinedURL
	}
	req.Host = baseURL.Host

	// Reset body for retries via GetBody.
	if origReq.GetBody != nil {
		body, err := origReq.GetBody()
		if err != nil {
			return nil, false, werror.WrapWithContextParams(ctx, err, "failed to get request body for retry")
		}
		req.Body = body
	}

	// Compose middleware stack using an iterative chain for flat stack traces.
	// Shallow copy the http.Client so we can override Transport.
	clientCopy := *c.httpClient.Current()

	// Per-request timeout override via context.
	if timeout, ok := requestTimeoutFromContext(ctx); ok {
		clientCopy.Timeout = timeout
	}

	// Always block 307/308 redirects — the retrier handles these as QoS signals.
	// 301/302/303 are still followed normally by http.Client.
	clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
		if resp := redirectReq.Response; resp != nil {
			if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
				return http.ErrUseLastResponse
			}
		}
		return nil
	}

	// Wrap transport with middleware: innermost first, outermost last.
	// wrapTransport iterates forwards, wrapping each around the previous,
	// so the last element ends up outermost.
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.uriScorer)      // innermost
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.middlewares...) // user MWs: last added = outermost
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.recoveryMW)     // outermost

	// Execute the request.
	resp, respErr := clientCopy.Do(req)
	if respErr != nil {
		return nil, isRetryableBody(origReq), unwrapURLError(ctx, respErr)
	}
	return resp, resp.StatusCode >= 300 && isRetryableBody(origReq), nil
}

// isRetryableBody reports whether the request body is replayable (or absent),
// meaning the request can be safely retried.
func isRetryableBody(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

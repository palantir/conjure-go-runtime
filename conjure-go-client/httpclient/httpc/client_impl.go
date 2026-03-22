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
type fluentClient[B ServiceBuilder[B]] struct {
	serviceName    refreshable.Refreshable[string]
	httpClient     refreshable.Refreshable[*http.Client]
	middlewares    []Middleware
	errorDecoderMW Middleware // client-level error decoder as middleware
	recoveryMW     Middleware
	uriScorer      internal.RefreshableURIScoringMiddleware
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	bufferPool     bytesbuffers.Pool

	// retains a reference to the builder for ConfigurableClient.Builder().
	builder B
}

// getBufferPool returns the client's buffer pool, implementing poolProvider.
func (c *fluentClient[B]) getBufferPool() bytesbuffers.Pool {
	return c.bufferPool
}

func (c *fluentClient[B]) Do(req *http.Request) (*http.Response, error) {
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
		if err != nil {
			svc1log.FromContext(ctx).Debug("Retrying request", svc1log.Stacktrace(err))
		}
	}
}

func (c *fluentClient[B]) doOnce(
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

	// Build middleware slice: outermost (recovery) first, innermost (URI scorer) last.
	mws := make([]Middleware, 0, 3+len(c.middlewares))
	mws = append(mws, c.recoveryMW)
	mws = append(mws, c.middlewares...)
	mws = append(mws, c.errorDecoderMW)
	mws = append(mws, c.uriScorer)

	clientCopy.Transport = &middlewareChain{middlewares: mws, base: clientCopy.Transport}

	// Execute the request.
	resp, respErr := clientCopy.Do(req)

	if respErr != nil {
		// Request can be retried if body is replayable.
		if origReq.GetBody != nil {
			retryable = true
		}
		return nil, retryable, unwrapURLError(ctx, respErr)
	}
	return resp, false, nil
}

func (c *fluentClient[B]) Builder() B {
	return c.builder.Clone()
}

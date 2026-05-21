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
	"strings"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// fluentClient implements Client by wrapping an *http.Client with retry and URI scoring.
type fluentClient struct {
	serviceName    refreshable.Refreshable[string]
	httpClient     refreshable.Refreshable[*http.Client]
	middlewares    []Middleware
	errorDecoder   ErrorDecoder
	recoveryMW     Middleware
	uriScorer      internal.URIScoringMiddleware
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	bufferPool     bytesbuffers.Pool
}

// configurableClient retains the builder so ConfigurableClient.Builder() can return a clone.
type configurableClient[B ServiceBuilder[B]] struct {
	fluentClient
	builder B
}

func (c *configurableClient[B]) Builder() B {
	return c.builder.Clone()
}

func (c *fluentClient) getBufferPool() bytesbuffers.Pool {
	return c.bufferPool
}

// errorDecoderProvider lets Endpoint.Execute extract the client-level decoder
// before wrapping the client in per-endpoint middleware.
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
		drainBody(ctx, resp)
		if err != nil {
			svc1log.FromContext(ctx).Debug("Retrying request", svc1log.Stacktrace(err))
		} else if resp != nil {
			svc1log.FromContext(ctx).Debug("Retrying request", svc1log.SafeParam("statusCode", resp.StatusCode))
		}
	}
}

func (c *fluentClient) doOnce(
	origReq *http.Request,
	baseURI string,
	useBaseURIOnly bool,
) (_ *http.Response, retryable bool, _ error) {
	ctx := origReq.Context()
	req := origReq.Clone(ctx)

	baseURL, err := url.Parse(baseURI)
	if err != nil {
		return nil, false, werror.WrapWithContextParams(ctx, err, "invalid URL")
	}
	if useBaseURIOnly {
		req.URL = new(*baseURL)
	} else {
		joinedURL, err := joinBaseAndRequestURL(baseURL, origReq.URL)
		if err != nil {
			return nil, false, werror.WrapWithContextParams(ctx, err, "failed to construct request URL")
		}
		req.URL = joinedURL
	}
	req.Host = baseURL.Host

	// Reset the body via GetBody so each retry starts from the beginning.
	if origReq.GetBody != nil {
		body, err := origReq.GetBody()
		if err != nil {
			return nil, false, werror.WrapWithContextParams(ctx, err, "failed to get request body for retry")
		}
		req.Body = body
	}

	// Shallow-copy the http.Client so this attempt can override Transport and Timeout.
	clientCopy := *c.httpClient.Current()
	if timeout, ok := requestTimeoutFromContext(ctx); ok {
		clientCopy.Timeout = timeout
	}

	// Block 307/308 — the retrier treats those as Conjure QoS redirects.
	// 301/302/303 are still followed by http.Client.
	clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
		if resp := redirectReq.Response; resp != nil {
			if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
				return http.ErrUseLastResponse
			}
		}
		return nil
	}

	// Wrap iteratively (last element is outermost).
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.uriScorer)      // innermost
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.middlewares...) // user middlewares
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.recoveryMW)     // outermost

	resp, respErr := clientCopy.Do(req)
	if respErr != nil {
		return nil, isRetryableBody(origReq), unwrapURLError(ctx, respErr)
	}
	return resp, resp.StatusCode >= 300 && isRetryableBody(origReq), nil
}

// isRetryableBody reports whether the request body is replayable (absent or has GetBody).
func isRetryableBody(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

func joinBaseAndRequestURL(baseURL, reqURL *url.URL) (*url.URL, error) {
	joined := new(*baseURL)
	if escapedPath := reqURL.EscapedPath(); escapedPath != "" {
		if basePath := baseURL.EscapedPath(); basePath != "" {
			escapedPath = strings.TrimRight(basePath, "/") + "/" + strings.TrimLeft(escapedPath, "/")
		}
		path, err := url.PathUnescape(escapedPath)
		if err != nil {
			return nil, err
		}
		joined.Path = path
		joined.RawPath = ""
		if (&url.URL{Path: path}).EscapedPath() != escapedPath {
			joined.RawPath = escapedPath
		}
	}

	joined.RawQuery = reqURL.RawQuery
	return joined, nil
}

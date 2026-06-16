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
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// Client is the transport interface returned by [Builder.Build]. Its signature
// matches *http.Client.Do.
//
// A built Client prepends a selected base URL (per URI scoring) to the request
// path on each attempt, applies the middleware stack, enforces per-attempt
// timeouts, and retries replayable requests. Endpoint.Execute is the typical
// caller; it builds the request and decodes the response after Do returns.
//
// A plain *http.Client satisfies the interface but is only useful when the
// Endpoint's path template is an absolute URL — [Endpoint.Execute] emits
// path-only requests on the assumption that the Client prepends a base URL.
// Use [Builder.Build] for retries, URI scoring, per-attempt timeouts, and
// middleware.
type Client interface {
	Do(req *http.Request) (*http.Response, error)
}

// ConfigurableClient is a Client that exposes a fresh Builder seeded with its
// configuration, allowing reconfiguration without starting from scratch:
//
//	newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
//
// The type parameter B preserves the concrete builder type so downstream code
// that defines a custom builder satisfying ClientBuilder[*MyBuilder] gets back
// *MyBuilder rather than *Builder.
//
// A Client wrapper (test middleware, recording transport, retry adapter, etc.)
// does NOT automatically satisfy ConfigurableClient — the type assertion fails
// silently. Wrappers that want callers to reach the underlying builder should
// implement Builder() themselves, typically forwarding to the wrapped Client.
type ConfigurableClient[B ServiceBuilder[B]] interface {
	Client
	Builder() B
}

// fluentClient implements Client by wrapping an *http.Client with retry and URI scoring.
type fluentClient struct {
	serviceName    refreshable.Refreshable[string]
	httpClient     refreshable.Refreshable[*http.Client]
	uriScorer      URLSelector
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
}

// configurableClient retains the builder so ConfigurableClient.Builder() can return a clone.
type configurableClient[B ServiceBuilder[B]] struct {
	fluentClient
	builder B
}

func (c *configurableClient[B]) Builder() B {
	return c.builder.Clone()
}

func (c *fluentClient) Do(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	uris := c.uriScorer.BaseURLs()
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
	firstAttempt := true
	for {
		resp, retryable, err := c.doOnce(req, uri, isRelocated, firstAttempt)
		firstAttempt = false
		if !retryable {
			return resp, err
		}
		uri, isRelocated = retrier.GetNextURI(resp, err)
		if uri == "" {
			return resp, err
		}
		internal.DrainBody(ctx, resp)
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
	firstAttempt bool,
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

	// The first attempt uses the encoder-created body on origReq. Later attempts
	// reset the body via GetBody so each retry starts from the beginning.
	if !firstAttempt && origReq.GetBody != nil {
		body, err := origReq.GetBody()
		if err != nil {
			return nil, false, werror.WrapWithContextParams(ctx, err, "failed to get request body for retry")
		}
		req.Body = body
	}

	// Shallow-copy the http.Client so this attempt can override Transport and Timeout.
	clientCopy := *c.httpClient.Current()
	if timeout, ok := internal.RequestTimeoutFromContext(ctx); ok {
		clientCopy.Timeout = timeout
	}

	// 307/308 are Conjure QoS redirects handled by the retrier; 301/302/303
	// remain http.Client's responsibility.
	clientCopy.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
		if resp := redirectReq.Response; resp != nil {
			if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
				return http.ErrUseLastResponse
			}
		}
		return nil
	}

	// The scorer wraps the baked stack so it observes each attempt's outcome.
	clientCopy.Transport = wrapTransport(clientCopy.Transport, c.uriScorer)

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

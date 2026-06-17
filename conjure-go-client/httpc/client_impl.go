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
	"context"
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

// Client is a single-attempt HTTP transport plus the configuration [Send] needs
// to drive a full request: a base-URL selector and a default call policy.
// [Builder.Build] returns one; callers reach it through [Send] or
// [Endpoint.Execute] rather than calling RoundTrip directly.
//
// RoundTrip performs ONE attempt with the full middleware stack baked in. It
// does not retry, score URLs, or follow QoS redirects — [Send] layers those on
// top. The interface is embeddable: a wrapper can override RoundTrip while
// forwarding URLSelector and CallPolicy to the wrapped Client.
type Client interface {
	http.RoundTripper
	// URLSelector orders the base URLs and observes each attempt's outcome.
	URLSelector() URLSelector
	// CallPolicy returns the client's default per-call policy. Callers may
	// overlay per-request overrides (e.g. a request timeout) before passing it
	// to Send.
	CallPolicy() CallPolicy
}

// RebuildableClient is a [Client] that can return a [Builder] seeded with its
// configuration, allowing reconfiguration without starting from scratch:
//
//	newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
//
// The type parameter B preserves the concrete builder type so downstream code
// that defines a custom builder gets back its own type rather than *Builder.
//
// A Client wrapper does NOT automatically satisfy RebuildableClient. Wrappers
// that want callers to reach the underlying builder should implement Builder()
// themselves, typically forwarding to the wrapped Client.
type RebuildableClient[B ServiceBuilder[B]] interface {
	Client
	Builder() B
}

// CallPolicy is the per-call orchestration snapshot [Send] consumes. A client's
// defaults come from [Client.CallPolicy]; callers overlay per-request overrides.
type CallPolicy struct {
	// Timeout bounds each attempt. Zero disables the per-attempt timeout; a
	// total-call deadline is the caller's responsibility via context.
	Timeout time.Duration
	// MaxAttempts caps the total number of attempts. Nil uses the default of
	// 2×len(base URLs).
	MaxAttempts *int
	// InitialBackoff and MaxBackoff bound the exponential retry backoff.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// standardClient is the [RebuildableClient] returned by [Builder.Build]. It
// holds the baked middleware stack as a static transport; refreshable behavior
// lives inside the middlewares (read per request) and in CallPolicy.
type standardClient[B ServiceBuilder[B]] struct {
	serviceName    refreshable.Refreshable[string]
	transport      http.RoundTripper
	uriScorer      URLSelector
	timeout        refreshable.Refreshable[time.Duration]
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	builder        B
}

func (c *standardClient[B]) RoundTrip(req *http.Request) (*http.Response, error) {
	return c.transport.RoundTrip(req)
}

func (c *standardClient[B]) URLSelector() URLSelector { return c.uriScorer }

func (c *standardClient[B]) CallPolicy() CallPolicy {
	var maxAttempts *int
	if c.maxAttempts != nil {
		maxAttempts = c.maxAttempts.Current()
	}
	return CallPolicy{
		Timeout:        c.timeout.Current(),
		MaxAttempts:    maxAttempts,
		InitialBackoff: c.initialBackoff.Current(),
		MaxBackoff:     c.maxBackoff.Current(),
	}
}

func (c *standardClient[B]) Builder() B { return c.builder.Clone() }

// inlinesRequestMiddleware marks a Client that applies per-request middlewares
// (carried on the request context) itself, inside its telemetry layer.
// [Endpoint.Execute] hands middlewares to such a client via the context; for any
// other Client it wraps RoundTrip instead. standardClient bakes a
// [requestMiddlewareApplier] seam for exactly this.
type inlinesRequestMiddleware interface {
	inlinesRequestMiddleware()
}

func (*standardClient[B]) inlinesRequestMiddleware() {}

// Send runs a path-only request to completion against client. It orders the
// base URLs via the client's [URLSelector], prepends the selected base to the
// path per attempt, retries replayable requests across the URLs under pol, and
// applies pol.Timeout to each attempt. Standard redirects (301/302/303) are
// followed by the call-scoped http.Client; 307/308 are handed back to the
// retrier as Conjure QoS relocations.
//
// Send returns the raw final response; callers decode errors and bodies. It is
// the loop [Endpoint.Execute] and the legacy httpclient bridge share.
func Send(ctx context.Context, client Client, req *http.Request, pol CallPolicy) (*http.Response, error) {
	uris := client.URLSelector().BaseURLs()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "")
	}

	attempts := 2 * len(uris)
	if pol.MaxAttempts != nil {
		attempts = *pol.MaxAttempts
	}

	backoff := retry.Start(ctx, retry.WithInitialBackoff(pol.InitialBackoff), retry.WithMaxBackoff(pol.MaxBackoff))
	retrier := internal.NewRequestRetrier(uris, backoff, attempts)

	// Call-scoped client: the selector wraps the client's single-attempt
	// RoundTrip so it observes every attempt (and redirect hop); the http.Client
	// follows standard redirects and enforces the per-attempt timeout.
	hc := &http.Client{
		Transport: wrapTransport(client, client.URLSelector()),
		Timeout:   pol.Timeout,
		// 307/308 are Conjure QoS redirects handled by the retrier; 301/302/303
		// remain the http.Client's responsibility.
		CheckRedirect: func(redirectReq *http.Request, _ []*http.Request) error {
			if resp := redirectReq.Response; resp != nil {
				if resp.StatusCode == http.StatusTemporaryRedirect || resp.StatusCode == http.StatusPermanentRedirect {
					return http.ErrUseLastResponse
				}
			}
			return nil
		},
	}

	uri, isRelocated := retrier.GetNextURI(nil, nil)
	firstAttempt := true
	for {
		resp, retryable, err := sendOnce(ctx, hc, req, uri, isRelocated, firstAttempt)
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

func sendOnce(
	ctx context.Context,
	hc *http.Client,
	origReq *http.Request,
	baseURI string,
	useBaseURIOnly bool,
	firstAttempt bool,
) (_ *http.Response, retryable bool, _ error) {
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

	resp, respErr := hc.Do(req)
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

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
	"slices"
	"strings"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// Client is the configuration [Send] needs to drive a full request: a raw
// transport, the intrinsic middleware stack, a base-URL selector, and a default
// call policy. [Builder.Build] returns one; callers reach it through [Send] or
// [Endpoint.Execute] rather than assembling the pieces themselves.
//
// [Send] composes the pieces per attempt as selector → Middleware → auth/header
// decoration → per-request middleware → Transport, then layers retries, URL
// scoring, and QoS redirects on top. Exposing the pieces (rather than a single
// RoundTrip) keeps [Send] in control of composition, so a hand-built request
// can't drop auth or telemetry.
type Client interface {
	// Transport performs one attempt with no middleware decoration. nil falls
	// back to http.DefaultTransport.
	Transport() http.RoundTripper
	// Middleware is the intrinsic stack baked by the builder (telemetry and user
	// middleware), applied around every attempt. nil means none. Auth and builder
	// headers are applied separately as request-value contributors.
	Middleware() Middleware
	// URLSelector orders the base URLs and observes each attempt's outcome.
	URLSelector() URLSelector
	// CallPolicy returns the client's default per-call policy. Callers may
	// overlay per-request overrides (e.g. a request timeout) in [SendOptions].
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

// SendOptions configures a single [Send] call: the per-call policy plus any
// per-request middlewares. The middlewares run innermost — below the client's
// intrinsic stack and its auth/header decoration, just above the transport — so
// a request can override auth, and they run on every attempt with the resolved URL.
type SendOptions struct {
	CallPolicy
	Middlewares []Middleware

	// headerValues and queryValues are per-request contributors [Send] resolves
	// onto every attempt, above the client's intrinsic header values. Populated
	// by [Endpoint.Execute]; the legacy httpclient bridge leaves them nil. They
	// are unexported to keep the per-request decoration in-package.
	headerValues []requestValue[http.Header]
	queryValues  []requestValue[url.Values]
}

// intrinsicValuer is the unexported capability a [Client] may implement to
// contribute its baked header values (auth, builder headers) to [Send]'s
// resolution. The builder's client implements it; custom Client implementations
// (test doubles, escape hatches) do not and correctly contribute none.
type intrinsicValuer interface {
	intrinsicHeaderValues() []requestValue[http.Header]
}

// standardClient is the [RebuildableClient] returned by [Builder.Build]. It
// holds the raw transport and the intrinsic middleware stack separately;
// refreshable behavior lives inside the middlewares (read per request) and in
// CallPolicy.
type standardClient[B ServiceBuilder[B]] struct {
	serviceName    refreshable.Refreshable[string]
	transport      http.RoundTripper
	middleware     Middleware
	headerValues   []requestValue[http.Header]
	uriScorer      URLSelector
	timeout        refreshable.Refreshable[time.Duration]
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	builder        B
}

func (c *standardClient[B]) Transport() http.RoundTripper { return c.transport }

func (c *standardClient[B]) Middleware() Middleware { return c.middleware }

func (c *standardClient[B]) intrinsicHeaderValues() []requestValue[http.Header] {
	return c.headerValues
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

// Send runs a path-only request to completion against client. It orders the
// base URLs via the client's [URLSelector], prepends the selected base to the
// path per attempt, retries replayable requests across the URLs under
// opts.CallPolicy, and applies opts.Timeout to each attempt. Standard redirects
// (301/302/303) are followed by the call-scoped http.Client; 307/308 are handed
// back to the retrier as Conjure QoS relocations.
//
// Each attempt is composed as selector → client.Middleware → auth/header
// decoration → opts.Middlewares → client.Transport, so the intrinsic stack
// (telemetry) and the auth/header contributors always run, while per-request
// middlewares run innermost — on every attempt with the resolved URL.
//
// Send returns the raw final response; callers decode errors and bodies. It is
// the loop [Endpoint.Execute] and the legacy httpclient bridge share.
func Send(ctx context.Context, client Client, req *http.Request, opts SendOptions) (*http.Response, error) {
	selector := client.URLSelector()
	uris := selector.BaseURLs()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "")
	}

	attempts := 2 * len(uris)
	if opts.MaxAttempts != nil {
		attempts = *opts.MaxAttempts
	}

	backoff := retry.Start(ctx, retry.WithInitialBackoff(opts.InitialBackoff), retry.WithMaxBackoff(opts.MaxBackoff))
	retrier := internal.NewRequestRetrier(uris, backoff, attempts)

	// Resolve the client's intrinsic header values (auth, builder headers) below
	// the per-request contributors so the latter win. The decoration applies them
	// per attempt, inside the intrinsic middleware (so telemetry recovers/metering
	// covers auth) and outside the per-request middlewares (so an imperative
	// WithMiddleware can still override on the wire).
	var intrinsic []requestValue[http.Header]
	if v, ok := client.(intrinsicValuer); ok {
		intrinsic = v.intrinsicHeaderValues()
	}
	var decoration Middleware
	if len(intrinsic) > 0 || len(opts.headerValues) > 0 || len(opts.queryValues) > 0 {
		decoration = decorationMiddleware{
			headerValues: slices.Concat(intrinsic, opts.headerValues),
			queryValues:  opts.queryValues,
		}
	}

	// Per-attempt transport (innermost to outermost): the raw transport, the
	// per-request middlewares, the auth/header decoration, the client's intrinsic
	// stack, then the selector so it observes every attempt (and redirect hop).
	// The http.Client follows standard redirects and enforces the per-attempt timeout.
	transport := wrapTransport(client.Transport(), opts.Middlewares...)
	transport = wrapTransport(transport, decoration)
	transport = wrapTransport(transport, client.Middleware())
	hc := &http.Client{
		Transport: wrapTransport(transport, selector),
		Timeout:   opts.Timeout,
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

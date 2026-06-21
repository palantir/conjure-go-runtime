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

// Client sends a request to a configured service and returns the response. It is
// the behavior-first contract callers actually need: [Builder.Build] returns the
// standard implementation, and [Endpoint.Execute] drives requests through it. A
// custom implementation (a test fake, a wrapper) need only implement Send.
//
// The standard implementation owns base-URL selection, retries, QoS redirects,
// telemetry, builder auth/header decoration, and per-attempt timeouts. It
// resolves opts.Values and runs opts.Middlewares per attempt on a freshly cloned
// request, so a hand-built request can't drop auth or telemetry and retries don't
// duplicate added values. The standard runtime treats req as path-only and
// prepends a selected base URL on each attempt.
type Client interface {
	Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error)
}

// RebuildableClient is a [Client] that can return a [Builder] seeded with its
// configuration, allowing reconfiguration without starting from scratch:
//
//	newClient, err := client.Builder().SetTimeout(5 * time.Second).Build(ctx)
//
// The type parameter B preserves the concrete builder type so downstream code
// that defines a custom builder gets back its own type rather than *Builder.
// B is bound only by the minimal [Cloneable] (Clone) contract, not the full
// [BuilderAPI]: the rebuild path only needs to clone the seed builder, so a
// custom builder need not satisfy every setter to be rebuildable.
//
// A Client wrapper does NOT automatically satisfy RebuildableClient. Wrappers
// that want callers to reach the underlying builder should implement Builder()
// themselves, typically forwarding to the wrapped Client.
type RebuildableClient[B Cloneable[B]] interface {
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

// SendOptions is the per-send configuration for [Client.Send]: request
// decoration (headers/query/basic auth), per-request middlewares, and call-policy
// overrides. The standard runtime resolves Values per attempt above its
// builder-intrinsic values, runs Middlewares innermost (just above the transport,
// so a request can override auth), and merges Policy onto its defaults. The zero
// value is valid.
type SendOptions struct {
	Values      RequestValues
	Middlewares []Middleware
	Policy      CallPolicyOverrides
}

// standardClient is the [RebuildableClient] returned by [Builder.Build]. It
// holds the raw transport and the intrinsic middleware stack separately;
// refreshable behavior lives inside the middlewares (read per request) and in
// CallPolicy.
type standardClient[B Cloneable[B]] struct {
	serviceName    refreshable.Refreshable[string]
	transport      http.RoundTripper
	middleware     Middleware
	intrinsic      RequestValues
	uriScorer      URLSelector
	timeout        refreshable.Refreshable[time.Duration]
	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]
	builder        B
}

func (c *standardClient[B]) Builder() B { return c.builder.Clone() }

// callPolicy snapshots the runtime's current default policy from its refreshable
// settings; [SendOptions.Policy] overrides are applied on top per send.
func (c *standardClient[B]) callPolicy() CallPolicy {
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

// Send runs a path-only request to completion: it orders the base URLs via the
// runtime's URL selector, prepends the selected base to the path per attempt,
// retries replayable requests across the URLs under the resolved [CallPolicy],
// and applies the per-attempt timeout. Standard redirects (301/302/303) are
// followed by the call-scoped http.Client; 307/308 are handed back to the
// retrier as Conjure QoS relocations.
//
// Each attempt is composed as selector → intrinsic middleware → auth/header
// decoration → opts.Middlewares → transport, so the intrinsic stack (telemetry)
// and the auth/header contributors always run, while per-request middlewares run
// innermost — on every attempt with the resolved URL. Decoration resolves the
// runtime's builder-intrinsic values below opts.Values (so per-call values win)
// per attempt on the freshly cloned request, so retries never duplicate added
// values and an overridden lazy auth provider never runs.
func (c *standardClient[B]) Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error) {
	selector := c.uriScorer
	uris := selector.BaseURLs()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "")
	}

	policy := opts.Policy.applyTo(c.callPolicy())
	attempts := 2 * len(uris)
	if policy.MaxAttempts != nil {
		attempts = *policy.MaxAttempts
	}

	backoff := retry.Start(ctx, retry.WithInitialBackoff(policy.InitialBackoff), retry.WithMaxBackoff(policy.MaxBackoff))
	retrier := internal.NewRequestRetrier(uris, backoff, attempts)

	// Resolve the runtime's intrinsic values (auth, builder headers) below the
	// per-call contributors so the latter win. Decoration applies them per attempt,
	// inside the intrinsic middleware (so telemetry recovery/metering covers auth)
	// and outside the per-request middlewares (so an imperative WithMiddleware can
	// still override on the wire).
	values := c.intrinsic.concat(opts.Values)
	var decoration Middleware
	if !values.isEmpty() {
		decoration = decorationMiddleware{
			headerValues: values.headerValues,
			queryValues:  values.queryValues,
		}
	}

	// Per-attempt transport (innermost to outermost): the raw transport, the
	// per-request middlewares, the auth/header decoration, the intrinsic stack,
	// then the selector so it observes every attempt (and redirect hop). The
	// http.Client follows standard redirects and enforces the per-attempt timeout.
	transport := wrapTransport(c.transport, opts.Middlewares...)
	transport = wrapTransport(transport, decoration)
	transport = wrapTransport(transport, c.middleware)
	hc := &http.Client{
		Transport: wrapTransport(transport, selector),
		Timeout:   policy.Timeout,
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

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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/internal/retrier"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

// Runtime sends a request to a configured service and returns the response. It is
// the behavior-first contract callers actually need: [Builder.Build] returns the
// standard implementation, and [Call.Execute] drives requests through it. A
// custom implementation (a test fake, a wrapper) need only implement Send.
//
// The standard implementation owns base-URL selection, retries, QoS redirects,
// telemetry, builder auth/header decoration, and per-attempt timeouts. It
// resolves opts.Values and runs opts.Middlewares per attempt on a freshly cloned
// request, so a hand-built request can't drop auth or telemetry and retries don't
// duplicate added values. The standard runtime treats req as path-only and
// prepends a selected base URL on each attempt.
type Runtime interface {
	Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error)
}

// RebuildableRuntime is a [Runtime] that can return a [Builder] seeded with its
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
// A Runtime wrapper does NOT automatically satisfy RebuildableRuntime. Wrappers
// that want callers to reach the underlying builder should implement Builder()
// themselves, typically forwarding to the wrapped Runtime.
type RebuildableRuntime[B Cloneable[B]] interface {
	Runtime
	Builder() B
}

// callPolicy is the per-call orchestration snapshot the standard runtime resolves
// for a send: its refreshable defaults with any [CallPolicyOverrides] from
// [SendOptions] applied on top.
type callPolicy struct {
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

// CallPolicyOverrides is a per-send override set merged onto a runtime's default
// callPolicy. Unlike callPolicy (a final snapshot), each field tracks whether
// it was set, because zero/nil values are meaningful: a zero timeout disables the
// per-attempt timeout, and a nil max-attempts means "use the default formula".
// An unset field leaves the runtime default unchanged. The zero value overrides
// nothing.
type CallPolicyOverrides struct {
	timeout        *time.Duration
	initialBackoff *time.Duration
	maxBackoff     *time.Duration
	maxAttempts    *int
	maxAttemptsSet bool
}

// WithTimeout overrides the per-attempt timeout. A zero duration explicitly
// disables the per-attempt timeout (distinct from leaving it unset).
func (p CallPolicyOverrides) WithTimeout(d time.Duration) CallPolicyOverrides {
	p.timeout = &d
	return p
}

// WithMaxAttempts overrides total attempts. nil = default (2 per base URL);
// pointer to 0 = unlimited; n > 0 = exactly n. Calling this marks max attempts as
// overridden even when n is nil.
func (p CallPolicyOverrides) WithMaxAttempts(n *int) CallPolicyOverrides {
	p.maxAttempts = n
	p.maxAttemptsSet = true
	return p
}

// WithInitialBackoff overrides the initial retry backoff.
func (p CallPolicyOverrides) WithInitialBackoff(d time.Duration) CallPolicyOverrides {
	p.initialBackoff = &d
	return p
}

// WithMaxBackoff overrides the maximum retry backoff.
func (p CallPolicyOverrides) WithMaxBackoff(d time.Duration) CallPolicyOverrides {
	p.maxBackoff = &d
	return p
}

// applyTo returns base with each explicitly-set override applied.
func (p CallPolicyOverrides) applyTo(base callPolicy) callPolicy {
	if p.timeout != nil {
		base.Timeout = *p.timeout
	}
	if p.initialBackoff != nil {
		base.InitialBackoff = *p.initialBackoff
	}
	if p.maxBackoff != nil {
		base.MaxBackoff = *p.maxBackoff
	}
	if p.maxAttemptsSet {
		base.MaxAttempts = p.maxAttempts
	}
	return base
}

// SendOptions is the per-send configuration for [Runtime.Send]: request
// decoration (headers/query/authorization), per-request middlewares, and call-policy
// overrides. The standard runtime resolves Values per attempt above its
// builder-intrinsic values, runs Middlewares innermost (just above the transport,
// so a request can override auth), and merges Policy onto its defaults. The zero
// value is valid.
type SendOptions struct {
	Values      RequestValues
	Middlewares []Middleware
	Policy      CallPolicyOverrides
}

// ErrEmptyURIs is returned by Build and by [Runtime.Send] when the client has no
// configured base URIs.
type ErrEmptyURIs struct{}

func (ErrEmptyURIs) Error() string {
	return "httpc: base URLs must not be empty"
}

// ErrNonRelativeRequestURL is returned by [Runtime.Send] when the request URL is
// not service-relative. The standard runtime supplies the scheme, host, and port
// from the selected base URL on each attempt, so only the request URL's Path,
// RawPath, and RawQuery are meaningful; a scheme, host, or opaque URL would be
// silently discarded, so it is rejected instead. (Fragment and userinfo are
// likewise never sent, but are ignored rather than rejected.)
type ErrNonRelativeRequestURL struct{}

func (ErrNonRelativeRequestURL) Error() string {
	return "httpc: request URL must be relative (path only); the runtime supplies scheme and host per attempt"
}

// ErrInvalidRelocation is returned by [Runtime.Send] when a server's 307/308 QoS
// relocation (RetryOther / RetryTemporaryRedirect) points to a Location outside the
// configured service targets — a different scheme, host, port, or base path. The
// relocation is refused rather than followed, so a compromised or buggy upstream cannot
// pivot the client (and its replayable request body) onto an arbitrary host. The
// offending Location is attached as the unsafe 'location' parameter.
type ErrInvalidRelocation struct{}

func (ErrInvalidRelocation) Error() string {
	return "httpc: server relocated the request (307/308) to a host outside the configured service targets"
}

// standardRuntime is the [RebuildableRuntime] returned by [Builder.Build]. It
// holds the raw transport and the intrinsic middleware stack separately;
// refreshable behavior lives inside the middlewares (read per request) and in
// the call-policy fields.
type standardRuntime[B Cloneable[B]] struct {
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

func (c *standardRuntime[B]) Builder() B { return c.builder.Clone() }

// defaultCallPolicy snapshots the runtime's current default policy from its
// refreshable settings; [SendOptions.Policy] overrides are applied on top per send.
func (c *standardRuntime[B]) defaultCallPolicy() callPolicy {
	var maxAttempts *int
	if c.maxAttempts != nil {
		maxAttempts = c.maxAttempts.Current()
	}
	return callPolicy{
		Timeout:        c.timeout.Current(),
		MaxAttempts:    maxAttempts,
		InitialBackoff: c.initialBackoff.Current(),
		MaxBackoff:     c.maxBackoff.Current(),
	}
}

// Send runs a path-only request to completion: it orders the base URLs via the
// runtime's URL selector, prepends the selected base to the path per attempt,
// retries replayable requests across the URLs under the resolved call policy,
// and applies the per-attempt timeout. Standard redirects (301/302/303) are
// followed by the call-scoped http.Client; 307/308 are handed back to the
// retrier as Conjure QoS relocations.
//
// Each attempt is composed as selector → intrinsic middleware → auth/header
// decoration → opts.Middlewares → transport, so the intrinsic stack (telemetry)
// and the auth/header contributors always run, while per-request middlewares run
// innermost — on every attempt with the resolved URL. Decoration resolves the
// runtime's builder-intrinsic values below any headers set directly on req and
// below opts.Values (so request headers and per-call values both win over builder
// auth/headers and suppress an overridden lazy auth provider) per attempt on the
// freshly cloned request, so retries never duplicate added values.
func (c *standardRuntime[B]) Send(ctx context.Context, req *http.Request, opts SendOptions) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, werror.ErrorWithContextParams(ctx, "httpc: request and request URL must be non-nil")
	}
	// Reject a non-relative URL rather than silently discarding its scheme/host:
	// the runtime supplies scheme/host/port from the selected base URL per attempt
	// (only Path/RawPath/RawQuery are honored). Covers absolute, scheme-relative
	// (//host/path), and opaque (scheme:opaque) URLs.
	if req.URL.Scheme != "" || req.URL.Host != "" || req.URL.Opaque != "" {
		return nil, werror.WrapWithContextParams(ctx, ErrNonRelativeRequestURL{}, "", werror.UnsafeParam("url", req.URL.Redacted()))
	}

	selector := c.uriScorer
	uris := selector.BaseURLs()
	if len(uris) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "")
	}
	// Normalized configured targets, shared by the relocation routing gate (below) and
	// the auth gate (decoration). Built once: the base URL set is fixed for this send.
	targets := configuredTargetsFromURIs(uris)

	policy := opts.Policy.applyTo(c.defaultCallPolicy())
	attempts := 2 * len(uris)
	if policy.MaxAttempts != nil {
		attempts = *policy.MaxAttempts
	}

	backoff := retry.Start(ctx, retry.WithInitialBackoff(policy.InitialBackoff), retry.WithMaxBackoff(policy.MaxBackoff))
	retrier := retrier.NewRequestRetrier(uris, backoff, attempts)

	// Resolve decoration per attempt, lowest precedence first: the runtime's
	// intrinsic values (builder auth/headers), then any headers the caller set
	// directly on req, then opts.Values. Hoisting req's headers above the intrinsic
	// layer means a header set on the request (like opts.Values) beats builder auth
	// and suppresses the intrinsic auth provider, matching Call.Execute — rather than
	// being silently overwritten by builder decoration. (req arrives header-empty from
	// Call.Execute and the httpclient bridge, so the hoist is a no-op there.)
	// Decoration runs inside the intrinsic middleware (so telemetry recovery/metering
	// covers auth) and outside the per-request middlewares (so an imperative
	// WithMiddleware can still override on the wire), resolved per attempt on the
	// freshly cloned request so retries never duplicate added values.
	values := c.intrinsic.concat(requestValuesFromHeader(req.Header)).concat(opts.Values)
	var decoration Middleware
	if !values.isEmpty() {
		decoration = decorationMiddleware{
			headerValues:      values.headerValues,
			queryValues:       values.queryValues,
			authorizedTargets: targets,
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
		// Confine a 307/308 QoS relocation to a configured target. The retrier follows the
		// server-supplied Location blindly; refusing an off-target relocation here (rather
		// than dispatching the full request and replayable body to it) closes the SSRF
		// pivot. A failover to the next configured node (no Location) is not a relocation.
		if isRelocated && !relocationAllowed(uri, targets) {
			drainBody(ctx, resp)
			return nil, werror.WrapWithContextParams(ctx, ErrInvalidRelocation{}, "", werror.UnsafeParam("location", uri))
		}
		drainBody(ctx, resp)
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

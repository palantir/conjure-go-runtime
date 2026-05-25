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
	"encoding/base64"
	"net/http"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ServiceBuilder is the largest slice of [ClientBuilder]: identity, base URLs,
// auth, headers, middleware, timeouts, retry, metrics, tracing, and error
// handling. Settings often have refreshable counterparts (SetFooRefreshable)
// for runtime updates; Clone preserves the refreshable link, while a static
// SetFoo replaces it with a fixed value.
//
// Refreshable counterparts exist only for settings surfaced through
// [ClientConfig] (and therefore tunable from external configuration). The
// no-argument Disable* setters (DisableTracing, DisableTraceHeaderPropagation,
// DisablePanicRecovery, DisableClientTraceMetrics) are code-API-only and have
// no refreshable variant by design.
type ServiceBuilder[B ServiceBuilder[B]] interface {
	Clone() B
	Apply(...Param[B]) B

	// Build returns a [ConfigurableClient]. Errors if required settings (e.g.,
	// base URLs) are missing.
	Build(ctx context.Context) (ConfigurableClient[B], error)

	// SetServiceName sets the logical service name used in metrics and logs.
	SetServiceName(string) B
	SetServiceNameRefreshable(refreshable.Refreshable[string]) B

	// SetBaseURLs sets the base URLs for the service.
	SetBaseURLs(...string) B
	SetBaseURLsRefreshable(refreshable.Refreshable[[]string]) B
	// SetAllowCreateWithEmptyURIs lets Build succeed with no URIs (requests then fail with ErrEmptyURIs).
	SetAllowCreateWithEmptyURIs(bool) B
	// SetURIScoringStrategy selects among multiple base URIs.
	SetURIScoringStrategy(URIScoringStrategy) B

	// SetAuthToken sets a static bearer token.
	SetAuthToken(string) B
	SetAuthTokenProvider(TokenProvider) B
	// SetAuthTokenRefreshable supplies a refreshable token; nil *string disables auth.
	SetAuthTokenRefreshable(refreshable.Refreshable[*string]) B
	SetBasicAuth(user, password string) B
	SetBasicAuthProvider(BasicAuthProvider) B
	// SetBasicAuthOptionalProvider installs a provider that may return nil to skip auth this request.
	SetBasicAuthOptionalProvider(BasicAuthOptionalProvider) B
	// SetBasicAuthRefreshable supplies refreshable credentials; nil *BasicAuth disables auth.
	SetBasicAuthRefreshable(refreshable.Refreshable[*BasicAuth]) B

	// AddHeader appends one or more values to a header. Multiple values for one key are allowed.
	AddHeader(key, value string, additionalValues ...string) B
	// SetHeader replaces all values for the key with the given value(s).
	SetHeader(key, value string, additionalValues ...string) B
	SetUserAgent(string) B
	// SetOverrideRequestHost overrides the Host header on all requests.
	SetOverrideRequestHost(string) B

	// AddMiddleware appends an outer middleware; the last added is outermost.
	AddMiddleware(Middleware) B
	// AddInnerMiddleware prepends an inner middleware (closest to the transport).
	AddInnerMiddleware(Middleware) B

	// SetTimeout sets the per-attempt timeout. The retry loop resets the timer
	// on each attempt; total wall-clock time may exceed this value. Default: 60s.
	SetTimeout(time.Duration) B
	SetTimeoutRefreshable(refreshable.Refreshable[time.Duration]) B

	// SetMaxAttempts sets total attempts (initial + retries). nil = default
	// (2 per base URL); pointer to 0 = unlimited; n > 0 = exactly n.
	SetMaxAttempts(*int) B
	SetMaxAttemptsRefreshable(refreshable.Refreshable[*int]) B
	// SetInitialBackoff sets the initial retry backoff. Default: 250ms.
	SetInitialBackoff(time.Duration) B
	SetInitialBackoffRefreshable(refreshable.Refreshable[time.Duration]) B
	// SetMaxBackoff sets the maximum retry backoff. Default: 2s.
	SetMaxBackoff(time.Duration) B
	SetMaxBackoffRefreshable(refreshable.Refreshable[time.Duration]) B

	// SetMetrics enables request metrics and appends the given tag providers.
	SetMetrics(...TagsProvider) B
	SetDisableMetrics(bool) B
	SetDisableMetricsRefreshable(refreshable.Refreshable[bool]) B

	// DisableTracing disables per-request span creation.
	DisableTracing() B
	// DisableTraceHeaderPropagation disables outbound B3 trace headers.
	DisableTraceHeaderPropagation() B
	// DisableClientTraceMetrics suppresses detailed metrics gathered via httptrace.ClientTrace.
	DisableClientTraceMetrics() B

	// DisablePanicRecovery disables the middleware-chain panic recovery layer.
	DisablePanicRecovery() B
}

// SetServiceName sets the logical service name used in metrics tags and log fields.
func (b *Builder) SetServiceName(s string) *Builder {
	b.serviceName = refreshable.New(s)
	return b
}

func (b *Builder) SetServiceNameRefreshable(r refreshable.Refreshable[string]) *Builder {
	b.serviceName = r
	return b
}

// SetBaseURLs sets the base URLs; each request is prefixed with one chosen by
// the URI scoring strategy.
func (b *Builder) SetBaseURLs(urls ...string) *Builder {
	b.uris = refreshable.New(urls)
	return b
}

func (b *Builder) SetBaseURLsRefreshable(r refreshable.Refreshable[[]string]) *Builder {
	b.uris = r
	return b
}

// SetAllowCreateWithEmptyURIs allows Build to succeed with no URIs configured.
// Requests then fail with [ErrEmptyURIs] until URIs are supplied.
func (b *Builder) SetAllowCreateWithEmptyURIs(allow bool) *Builder {
	b.allowEmptyURIs = allow
	return b
}

// SetURIScoringStrategy selects among multiple base URIs. URIScoringBalanced
// (default) prefers faster/healthier hosts; URIScoringRandom is uniform.
func (b *Builder) SetURIScoringStrategy(s URIScoringStrategy) *Builder {
	switch s {
	case URIScoringRandom:
		b.uriScorerBuilder = func(uris []string) internal.URIScoringMiddleware {
			return internal.NewRandomURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
	default:
		b.uriScorerBuilder = nil
	}
	return b
}

// authHeaderFunc returns the Authorization header value, or "" to leave the
// header unset.
type authHeaderFunc func(ctx context.Context) (string, error)

// authHeaderMiddleware sets Authorization from provider unless the header is
// already set on the request or the provider returns empty.
func authHeaderMiddleware(provider authHeaderFunc) Middleware {
	return MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if req.Header.Get("Authorization") == "" {
			v, err := provider(req.Context())
			if err != nil {
				return nil, err
			}
			if v != "" {
				req.Header.Set("Authorization", v)
			}
		}
		return next.RoundTrip(req)
	})
}

func bearerAuthHeader(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func basicAuthHeader(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// SetAuthToken sets a static bearer token, sent as "Authorization: Bearer <token>"
// unless the request already has an Authorization header.
func (b *Builder) SetAuthToken(t string) *Builder {
	b.authHeader = func(context.Context) (string, error) {
		return bearerAuthHeader(t), nil
	}
	return b
}

func (b *Builder) SetAuthTokenProvider(p TokenProvider) *Builder {
	b.authHeader = func(ctx context.Context) (string, error) {
		token, err := p(ctx)
		if err != nil {
			return "", err
		}
		return bearerAuthHeader(token), nil
	}
	return b
}

// SetAuthTokenRefreshable supplies a refreshable bearer token. A nil current
// value disables auth.
func (b *Builder) SetAuthTokenRefreshable(r refreshable.Refreshable[*string]) *Builder {
	b.authHeader = func(context.Context) (string, error) {
		s := r.Current()
		if s == nil {
			return "", nil
		}
		return bearerAuthHeader(*s), nil
	}
	return b
}

// SetBasicAuth sets static basic auth credentials.
func (b *Builder) SetBasicAuth(user, password string) *Builder {
	b.authHeader = func(context.Context) (string, error) {
		return basicAuthHeader(user, password), nil
	}
	return b
}

func (b *Builder) SetBasicAuthProvider(p BasicAuthProvider) *Builder {
	b.authHeader = func(ctx context.Context) (string, error) {
		auth, err := p(ctx)
		if err != nil {
			return "", err
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	}
	return b
}

// SetBasicAuthOptionalProvider installs a provider that may return nil to
// skip basic auth for an individual request (the Authorization header is left
// unset). Use this when auth is optional or conditional on request context.
func (b *Builder) SetBasicAuthOptionalProvider(p BasicAuthOptionalProvider) *Builder {
	b.authHeader = func(ctx context.Context) (string, error) {
		auth, err := p(ctx)
		if err != nil {
			return "", err
		}
		if auth == nil {
			return "", nil
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	}
	return b
}

// SetBasicAuthRefreshable supplies refreshable basic auth credentials. A nil
// current value disables auth.
func (b *Builder) SetBasicAuthRefreshable(r refreshable.Refreshable[*BasicAuth]) *Builder {
	b.authHeader = func(context.Context) (string, error) {
		auth := r.Current()
		if auth == nil {
			return "", nil
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	}
	return b
}

// AddHeader appends one or more values to a header on every request. For
// per-request headers, use [Overrides.WithAddedHeader] or [Endpoint.WithAddedHeader].
func (b *Builder) AddHeader(key, value string, additionalValues ...string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Add(key, value)
		for _, v := range additionalValues {
			req.Header.Add(key, v)
		}
		return next.RoundTrip(req)
	}))
}

// SetHeader sets a header on every request to the given value(s), replacing
// any prior values. For per-request headers, use [Overrides.WithHeader] or
// [Endpoint.WithHeader].
func (b *Builder) SetHeader(key, value string, additionalValues ...string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set(key, value)
		for _, v := range additionalValues {
			req.Header.Add(key, v)
		}
		return next.RoundTrip(req)
	}))
}

// SetUserAgent sets the User-Agent header on every request.
func (b *Builder) SetUserAgent(s string) *Builder {
	return b.SetHeader("User-Agent", s)
}

// SetOverrideRequestHost overrides Host on every request, decoupling it from
// the URL host (useful for virtual-host routing).
func (b *Builder) SetOverrideRequestHost(host string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Host = host
		return next.RoundTrip(req)
	}))
}

// AddMiddleware appends an outer middleware; the last added is outermost.
func (b *Builder) AddMiddleware(m Middleware) *Builder {
	b.middlewares = append(b.middlewares, m)
	return b
}

// AddInnerMiddleware prepends an inner middleware that runs inside metrics and
// tracing, closest to the transport.
func (b *Builder) AddInnerMiddleware(m Middleware) *Builder {
	b.innerMiddlewares = append([]Middleware{m}, b.innerMiddlewares...)
	return b
}

// SetTimeout sets the per-attempt timeout (applied as *http.Client.Timeout, so
// it covers the entire attempt including body reads). The retry loop resets
// the timer on each attempt. Default: 60s.
func (b *Builder) SetTimeout(d time.Duration) *Builder {
	b.timeout = refreshable.New(d)
	return b
}

func (b *Builder) SetTimeoutRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.timeout = r
	return b
}

// SetMaxAttempts sets total attempts (initial + retries). nil = default
// (2 per base URL); pointer to 0 = unlimited; n > 0 = exactly n. Negative
// values defer an error to Build.
func (b *Builder) SetMaxAttempts(p *int) *Builder {
	if p != nil && *p < 0 {
		b.errs = append(b.errs, werror.ErrorWithContextParams(context.Background(),
			"SetMaxAttempts: value must be nil, 0 (unlimited), or positive",
			werror.SafeParam("value", *p)))
		return b
	}
	b.maxAttempts = refreshable.New(p)
	return b
}

func (b *Builder) SetMaxAttemptsRefreshable(r refreshable.Refreshable[*int]) *Builder {
	b.maxAttempts = r
	return b
}

// SetInitialBackoff sets the initial retry backoff. Default: 250ms.
func (b *Builder) SetInitialBackoff(d time.Duration) *Builder {
	b.initialBackoff = refreshable.New(d)
	return b
}

func (b *Builder) SetInitialBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.initialBackoff = r
	return b
}

// SetMaxBackoff sets the maximum retry backoff. Default: 2s.
func (b *Builder) SetMaxBackoff(d time.Duration) *Builder {
	b.maxBackoff = refreshable.New(d)
	return b
}

func (b *Builder) SetMaxBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.maxBackoff = r
	return b
}

// SetMetrics enables request metrics and appends the given tag providers to
// any already installed (e.g. by [Builder.ApplyConfig]). See README.md for the
// full metrics catalog.
func (b *Builder) SetMetrics(providers ...TagsProvider) *Builder {
	b.disableMetrics = refreshable.New(false)
	b.metricsTagProviders = append(b.metricsTagProviders, providers...)
	return b
}

// SetDisableMetrics toggles request metric emission. Refreshable counterpart:
// [Builder.SetDisableMetricsRefreshable]. Refreshable support exists for this
// flag (and not the other Disable* setters) because metrics emission is
// surfaced in [MetricsConfig] and can be toggled from config at runtime.
func (b *Builder) SetDisableMetrics(disable bool) *Builder {
	b.disableMetrics = refreshable.New(disable)
	return b
}

func (b *Builder) SetDisableMetricsRefreshable(r refreshable.Refreshable[bool]) *Builder {
	b.disableMetrics = r
	return b
}

// DisableTracing disables per-request span creation. Trace header propagation
// is controlled separately via DisableTraceHeaderPropagation.
func (b *Builder) DisableTracing() *Builder {
	b.disableRequestSpan = true
	return b
}

// DisableTraceHeaderPropagation suppresses outbound B3 trace headers (X-B3-TraceId, etc.).
func (b *Builder) DisableTraceHeaderPropagation() *Builder {
	b.disableTraceHeaders = true
	return b
}

// DisableClientTraceMetrics suppresses detailed metrics using httptrace.ClientTrace.
func (b *Builder) DisableClientTraceMetrics() *Builder {
	b.disableTraceMetrics = true
	return b
}

// DisablePanicRecovery removes the outer panic-recovery middleware layer.
func (b *Builder) DisablePanicRecovery() *Builder {
	b.disableRecovery = true
	return b
}

// SetTransport injects a pre-built http.RoundTripper, bypassing the dialer,
// TLS, and transport builders. The middleware stack still wraps it.
func (b *Builder) SetTransport(rt http.RoundTripper) *Builder {
	b.transport = rt
	return b
}

// Build constructs a [ConfigurableClient]. Errors if no base URLs were set
// (unless [Builder.SetAllowCreateWithEmptyURIs] was called) or if any setter
// deferred a validation error (e.g., a malformed proxy URL).
func (b *Builder) Build(ctx context.Context) (ConfigurableClient[*Builder], error) {
	if err := builderErrors(ctx, b.errs); err != nil {
		return nil, err
	}
	if b.uris == nil {
		return nil, werror.ErrorWithContextParams(ctx, "httpclient URLs must be set in configuration or by constructor param", werror.SafeParam("serviceName", b.serviceName.Current()))
	}
	if !b.allowEmptyURIs && len(b.uris.Current()) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "", werror.SafeParam("serviceName", b.serviceName.Current()))
	}

	httpClient, err := b.BuildHTTPClient(ctx)
	if err != nil {
		return nil, err
	}

	var recovery Middleware
	if !b.disableRecovery {
		recovery = recoveryMiddleware{}
	}

	uriScorer := internal.NewRefreshableURIScoringMiddleware(b.uris, func(uris []string) internal.URIScoringMiddleware {
		if b.uriScorerBuilder == nil {
			return internal.NewBalancedURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
		return b.uriScorerBuilder(uris)
	})

	return &configurableClient[*Builder]{
		fluentClient: fluentClient{
			serviceName:    b.serviceName,
			httpClient:     httpClient,
			middlewares:    b.middlewares,
			recoveryMW:     recovery,
			uriScorer:      uriScorer,
			maxAttempts:    b.maxAttempts,
			initialBackoff: b.initialBackoff,
			maxBackoff:     b.maxBackoff,
		},
		builder: b.Clone(),
	}, nil
}

// BuildHTTPClient returns a refreshable *http.Client whose transport is
// wrapped (innermost to outermost) with auth, inner middlewares, metrics, and
// tracing. Panic recovery is not installed here — [Builder.Build] wraps it
// around the whole chain.
func (b *Builder) BuildHTTPClient(ctx context.Context) (refreshable.Refreshable[*http.Client], error) {
	transport, err := b.BuildTransport(ctx)
	if err != nil {
		return nil, err
	}

	// Wrap inside-out: auth runs closest to the transport, then user inner
	// middlewares, then metrics, then tracing.
	if b.authHeader != nil {
		transport = wrapTransport(transport, authHeaderMiddleware(b.authHeader))
	}
	transport = wrapTransport(transport, b.innerMiddlewares...)
	transport = wrapTransport(transport, &telemetryMiddleware{
		serviceName:         b.serviceName,
		disableMetrics:      b.disableMetrics,
		disableRequestSpan:  b.disableRequestSpan,
		disableTraceHeaders: b.disableTraceHeaders,
		disableTraceMetrics: b.disableTraceMetrics,
		tags:                b.metricsTagProviders,
	})
	mapped := refreshable.MapAuto(b.timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}
	})
	return mapped, nil
}

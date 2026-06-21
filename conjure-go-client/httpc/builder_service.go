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
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ServiceBuilder is the largest slice of [BuilderAPI]: identity, base URLs,
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
type ServiceBuilder[Self Cloneable[Self]] interface {
	Clone() Self
	Apply(...Param[Self]) Self

	// Build returns a [RebuildableRuntime]. Errors if required settings (e.g.,
	// base URLs) are missing.
	Build(ctx context.Context) (RebuildableRuntime[Self], error)

	// SetServiceName sets the logical service name used in metrics and logs.
	SetServiceName(string) Self
	SetServiceNameRefreshable(refreshable.Refreshable[string]) Self

	// SetBaseURLs sets the base URLs for the service.
	SetBaseURLs(...string) Self
	SetBaseURLsRefreshable(refreshable.Refreshable[[]string]) Self
	// SetAllowCreateWithEmptyURIs lets Build succeed with no URIs (requests then fail with ErrEmptyURIs).
	SetAllowCreateWithEmptyURIs(bool) Self
	// SetURLSelector sets the strategy for ordering base URLs per request. The
	// factory is invoked with the current base URLs (and again when they change).
	// Defaults to [BalancedURLSelector].
	SetURLSelector(func([]string) URLSelector) Self

	// SetAuthToken sets a static bearer token.
	SetAuthToken(string) Self
	SetAuthTokenProvider(TokenProvider) Self
	// SetAuthTokenRefreshable supplies a refreshable token; nil *string disables auth.
	SetAuthTokenRefreshable(refreshable.Refreshable[*string]) Self
	SetBasicAuth(user, password string) Self
	SetBasicAuthProvider(func(ctx context.Context) (BasicAuth, error)) Self
	// SetBasicAuthOptionalProvider installs a provider that may return nil to skip auth this request.
	SetBasicAuthOptionalProvider(func(ctx context.Context) (*BasicAuth, error)) Self
	// SetBasicAuthRefreshable supplies refreshable credentials; nil *BasicAuth disables auth.
	SetBasicAuthRefreshable(refreshable.Refreshable[*BasicAuth]) Self

	// AddHeader appends one or more values to a header. Multiple values for one key are allowed.
	AddHeader(key, value string, additionalValues ...string) Self
	// SetHeader replaces all values for the key with the given value(s).
	SetHeader(key, value string, additionalValues ...string) Self
	SetUserAgent(string) Self
	// SetOverrideRequestHost overrides the Host header on all requests.
	SetOverrideRequestHost(string) Self

	// AddMiddleware appends an outer middleware: inside telemetry, outside the
	// inner middleware. Last added is outermost.
	AddMiddleware(Middleware) Self
	// AddInnerMiddleware prepends an inner middleware that runs inside the outer
	// middleware, just outside the auth and header decoration.
	AddInnerMiddleware(Middleware) Self

	// SetTimeout sets the per-attempt timeout. The retry loop resets the timer
	// on each attempt; total wall-clock time may exceed this value. Default: 60s.
	SetTimeout(time.Duration) Self
	SetTimeoutRefreshable(refreshable.Refreshable[time.Duration]) Self

	// SetMaxAttempts sets total attempts (initial + retries). nil = default
	// (2 per base URL); pointer to 0 = unlimited; n > 0 = exactly n.
	SetMaxAttempts(*int) Self
	SetMaxAttemptsRefreshable(refreshable.Refreshable[*int]) Self
	// SetInitialBackoff sets the initial retry backoff. Default: 250ms.
	SetInitialBackoff(time.Duration) Self
	SetInitialBackoffRefreshable(refreshable.Refreshable[time.Duration]) Self
	// SetMaxBackoff sets the maximum retry backoff. Default: 2s.
	SetMaxBackoff(time.Duration) Self
	SetMaxBackoffRefreshable(refreshable.Refreshable[time.Duration]) Self

	// SetMetrics enables request metrics and appends the given tag providers.
	SetMetrics(...TagsProvider) Self
	SetDisableMetrics(bool) Self
	SetDisableMetricsRefreshable(refreshable.Refreshable[bool]) Self

	// DisableTracing disables per-request span creation.
	DisableTracing() Self
	// DisableTraceHeaderPropagation disables outbound B3 trace headers.
	DisableTraceHeaderPropagation() Self
	// DisableClientTraceMetrics suppresses detailed metrics gathered via httptrace.ClientTrace.
	DisableClientTraceMetrics() Self

	// DisablePanicRecovery disables the middleware-chain panic recovery layer.
	DisablePanicRecovery() Self
}

// SetServiceName sets the logical service name used in metrics tags and log fields.
func (b *BuilderCore[Self]) SetServiceName(s string) Self {
	b.serviceName = refreshable.New(s)
	return b.self
}

func (b *BuilderCore[Self]) SetServiceNameRefreshable(r refreshable.Refreshable[string]) Self {
	b.serviceName = r
	return b.self
}

// SetBaseURLs sets the base URLs; each request is prefixed with one chosen by
// the URI scoring strategy. Each URL is validated with the same URI parser as
// [Builder.ApplyConfig] (url.ParseRequestURI); unlike config, direct setters do
// not drop empty strings — an empty URL is invalid. An invalid URL defers an
// error to Build, replacing any prior base-URL error.
func (b *BuilderCore[Self]) SetBaseURLs(urls ...string) Self {
	b.errs.setField(fieldBaseURLs, validateBaseURIs(urls))
	b.uris = refreshable.New(urls)
	return b.self
}

// SetBaseURLsRefreshable supplies refreshable base URLs. The error deferred to
// Build is re-evaluated against the live value, so an initially-invalid source
// that updates to a valid value before Build no longer fails. Invalid refreshes
// are ignored: the live URI list retains the last valid value (matching
// [Builder.ApplyConfigRefreshable]) rather than poisoning the client.
func (b *BuilderCore[Self]) SetBaseURLsRefreshable(r refreshable.Refreshable[[]string]) Self {
	validated, _ := refreshable.ValidateAuto(context.Background(), r, func(_ context.Context, uris []string) error {
		return validateBaseURIs(uris)
	})
	b.errs.setFieldProvider(fieldBaseURLs, validatedBuilderError(validated))
	b.uris = refreshable.MapFromValidatedAuto(validated, func(uris []string) []string { return uris })
	return b.self
}

// SetAllowCreateWithEmptyURIs allows Build to succeed with no URIs configured.
// Requests then fail with [ErrEmptyURIs] until URIs are supplied.
func (b *BuilderCore[Self]) SetAllowCreateWithEmptyURIs(allow bool) Self {
	b.allowEmptyURIs = allow
	return b.self
}

// SetURLSelector sets the strategy for ordering base URLs per request. The
// factory is invoked with the current base URLs, and again whenever they change.
// Defaults to [BalancedURLSelector] when unset. Use [RandomURLSelector] for
// uniform ordering, or supply a custom [URLSelector] factory.
func (b *BuilderCore[Self]) SetURLSelector(factory func([]string) URLSelector) Self {
	b.urlSelectorFactory = factory
	return b.self
}

// authHeaderFunc returns the Authorization header value, or "" to leave the
// header unset. Wrapped in an [authValue] contributor, it runs only when no
// higher-precedence contributor (a request header or per-request basic auth)
// claims Authorization.
type authHeaderFunc func(ctx context.Context) (string, error)

// SetAuthToken sets a static bearer token, sent as "Authorization: Bearer <token>"
// unless the request already has an Authorization header.
func (b *BuilderCore[Self]) SetAuthToken(t string) Self {
	b.authHeader = func(context.Context) (string, error) {
		return bearerAuthHeader(t), nil
	}
	return b.self
}

func (b *BuilderCore[Self]) SetAuthTokenProvider(p TokenProvider) Self {
	b.authHeader = func(ctx context.Context) (string, error) {
		token, err := p(ctx)
		if err != nil {
			return "", err
		}
		return bearerAuthHeader(token), nil
	}
	return b.self
}

// SetAuthTokenRefreshable supplies a refreshable bearer token. A nil current
// value disables auth.
func (b *BuilderCore[Self]) SetAuthTokenRefreshable(r refreshable.Refreshable[*string]) Self {
	b.authHeader = func(context.Context) (string, error) {
		s := r.Current()
		if s == nil {
			return "", nil
		}
		return bearerAuthHeader(*s), nil
	}
	return b.self
}

// SetBasicAuth sets static basic auth credentials.
func (b *BuilderCore[Self]) SetBasicAuth(user, password string) Self {
	b.authHeader = func(context.Context) (string, error) {
		return basicAuthHeader(user, password), nil
	}
	return b.self
}

func (b *BuilderCore[Self]) SetBasicAuthProvider(p func(ctx context.Context) (BasicAuth, error)) Self {
	b.authHeader = func(ctx context.Context) (string, error) {
		auth, err := p(ctx)
		if err != nil {
			return "", err
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	}
	return b.self
}

// SetBasicAuthOptionalProvider installs a provider that may return nil to
// skip basic auth for an individual request (the Authorization header is left
// unset). Use this when auth is optional or conditional on request context.
func (b *BuilderCore[Self]) SetBasicAuthOptionalProvider(p func(ctx context.Context) (*BasicAuth, error)) Self {
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
	return b.self
}

// SetBasicAuthRefreshable supplies refreshable basic auth credentials. A nil
// current value disables auth.
func (b *BuilderCore[Self]) SetBasicAuthRefreshable(r refreshable.Refreshable[*BasicAuth]) Self {
	b.authHeader = func(context.Context) (string, error) {
		auth := r.Current()
		if auth == nil {
			return "", nil
		}
		return basicAuthHeader(auth.User, auth.Password), nil
	}
	return b.self
}

// AddHeader appends one or more values to a header on every request. For
// per-request headers, use [Overrides.WithAddedHeader] or [Call.WithAddedHeader].
func (b *BuilderCore[Self]) AddHeader(key, value string, additionalValues ...string) Self {
	b.headerValues = append(b.headerValues, addValue[http.Header]{
		name:   http.CanonicalHeaderKey(key),
		values: append([]string{value}, additionalValues...),
	})
	return b.self
}

// SetHeader sets a header on every request to the given value(s), replacing
// any prior values. For per-request headers, use [Overrides.WithHeader] or
// [Call.WithHeader]. A per-request header for the same key takes precedence.
func (b *BuilderCore[Self]) SetHeader(key, value string, additionalValues ...string) Self {
	b.headerValues = append(b.headerValues, setValue[http.Header]{
		name:   http.CanonicalHeaderKey(key),
		values: append([]string{value}, additionalValues...),
	})
	return b.self
}

// SetUserAgent sets the User-Agent header on every request.
func (b *BuilderCore[Self]) SetUserAgent(s string) Self {
	return b.SetHeader("User-Agent", s)
}

// SetOverrideRequestHost overrides Host on every request, decoupling it from
// the URL host (useful for virtual-host routing).
func (b *BuilderCore[Self]) SetOverrideRequestHost(host string) Self {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Host = host
		return next.RoundTrip(req)
	}))
}

// AddMiddleware appends an outer middleware: inside telemetry, outside the inner
// middleware. Last added is outermost.
func (b *BuilderCore[Self]) AddMiddleware(m Middleware) Self {
	b.middlewares = append(b.middlewares, m)
	return b.self
}

// AddInnerMiddleware prepends an inner middleware that runs inside the outer
// middleware, just outside the auth and header decoration.
func (b *BuilderCore[Self]) AddInnerMiddleware(m Middleware) Self {
	b.innerMiddlewares = append([]Middleware{m}, b.innerMiddlewares...)
	return b.self
}

// SetTimeout sets the per-attempt timeout (applied as *http.Client.Timeout, so
// it covers the entire attempt including body reads). The retry loop resets
// the timer on each attempt. Default: 60s.
func (b *BuilderCore[Self]) SetTimeout(d time.Duration) Self {
	b.timeout = refreshable.New(d)
	return b.self
}

func (b *BuilderCore[Self]) SetTimeoutRefreshable(r refreshable.Refreshable[time.Duration]) Self {
	b.timeout = r
	return b.self
}

// SetMaxAttempts sets total attempts (initial + retries). nil = default
// (2 per base URL); pointer to 0 = unlimited; n > 0 = exactly n. Negative
// values defer an error to Build.
func (b *BuilderCore[Self]) SetMaxAttempts(p *int) Self {
	b.errs.clearField(fieldMaxAttempts)
	if p != nil && *p < 0 {
		b.errs.setField(fieldMaxAttempts, werror.ErrorWithContextParams(context.Background(),
			"SetMaxAttempts: value must be nil, 0 (unlimited), or positive",
			werror.SafeParam("value", *p)))
		return b.self
	}
	b.maxAttempts = refreshable.New(p)
	return b.self
}

func (b *BuilderCore[Self]) SetMaxAttemptsRefreshable(r refreshable.Refreshable[*int]) Self {
	b.maxAttempts = r
	return b.self
}

// SetInitialBackoff sets the initial retry backoff. Default: 250ms.
func (b *BuilderCore[Self]) SetInitialBackoff(d time.Duration) Self {
	b.initialBackoff = refreshable.New(d)
	return b.self
}

func (b *BuilderCore[Self]) SetInitialBackoffRefreshable(r refreshable.Refreshable[time.Duration]) Self {
	b.initialBackoff = r
	return b.self
}

// SetMaxBackoff sets the maximum retry backoff. Default: 2s.
func (b *BuilderCore[Self]) SetMaxBackoff(d time.Duration) Self {
	b.maxBackoff = refreshable.New(d)
	return b.self
}

func (b *BuilderCore[Self]) SetMaxBackoffRefreshable(r refreshable.Refreshable[time.Duration]) Self {
	b.maxBackoff = r
	return b.self
}

// SetMetrics enables request metrics and appends the given tag providers to
// any already installed (e.g. by [Builder.ApplyConfig]). See README.md for the
// full metrics catalog.
func (b *BuilderCore[Self]) SetMetrics(providers ...TagsProvider) Self {
	b.disableMetrics = refreshable.New(false)
	b.metricsTagProviders = append(b.metricsTagProviders, providers...)
	return b.self
}

// SetDisableMetrics toggles request metric emission. Refreshable counterpart:
// [Builder.SetDisableMetricsRefreshable]. Refreshable support exists for this
// flag (and not the other Disable* setters) because metrics emission is
// surfaced in [MetricsConfig] and can be toggled from config at runtime.
func (b *BuilderCore[Self]) SetDisableMetrics(disable bool) Self {
	b.disableMetrics = refreshable.New(disable)
	return b.self
}

func (b *BuilderCore[Self]) SetDisableMetricsRefreshable(r refreshable.Refreshable[bool]) Self {
	b.disableMetrics = r
	return b.self
}

// DisableTracing disables per-request span creation. Trace header propagation
// is controlled separately via DisableTraceHeaderPropagation.
func (b *BuilderCore[Self]) DisableTracing() Self {
	b.disableRequestSpan = true
	return b.self
}

// DisableTraceHeaderPropagation suppresses outbound B3 trace headers (X-B3-TraceId, etc.).
func (b *BuilderCore[Self]) DisableTraceHeaderPropagation() Self {
	b.disableTraceHeaders = true
	return b.self
}

// DisableClientTraceMetrics suppresses detailed metrics using httptrace.ClientTrace.
func (b *BuilderCore[Self]) DisableClientTraceMetrics() Self {
	b.disableTraceMetrics = true
	return b.self
}

// DisablePanicRecovery removes the outer panic-recovery middleware layer.
func (b *BuilderCore[Self]) DisablePanicRecovery() Self {
	b.disableRecovery = true
	return b.self
}

// SetTransport injects a pre-built http.RoundTripper, bypassing the dialer,
// TLS, and transport builders. The middleware stack still wraps it.
func (b *BuilderCore[Self]) SetTransport(rt http.RoundTripper) Self {
	b.transport = rt
	return b.self
}

// Build constructs a [RebuildableRuntime]. Errors if no base URLs were set
// (unless [Builder.SetAllowCreateWithEmptyURIs] was called) or if any setter
// deferred a validation error (e.g., a malformed proxy URL).
func (b *BuilderCore[Self]) Build(ctx context.Context) (RebuildableRuntime[Self], error) {
	if err := b.errs.joined(ctx); err != nil {
		return nil, err
	}
	if b.uris == nil {
		return nil, werror.ErrorWithContextParams(ctx, "httpc: base URLs must be set in configuration or via SetBaseURLs", werror.SafeParam("serviceName", b.serviceName.Current()))
	}
	if !b.allowEmptyURIs && len(b.uris.Current()) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "", werror.SafeParam("serviceName", b.serviceName.Current()))
	}

	transport, err := b.BuildTransport(ctx)
	if err != nil {
		return nil, err
	}

	selectorFactory := b.urlSelectorFactory
	if selectorFactory == nil {
		selectorFactory = BalancedURLSelector
	}
	uriScorer := newRefreshableSelector(b.uris, selectorFactory)

	return &standardRuntime[Self]{
		serviceName:    b.serviceName,
		transport:      transport,
		middleware:     b.bakeMiddleware(),
		intrinsic:      RequestValues{headerValues: b.intrinsicHeaderValues()},
		uriScorer:      uriScorer,
		timeout:        b.timeout,
		maxAttempts:    b.maxAttempts,
		initialBackoff: b.initialBackoff,
		maxBackoff:     b.maxBackoff,
		builder:        b.self.Clone(),
	}, nil
}

// intrinsicHeaderValues returns the client's baked header contributors, lowest
// precedence first: the auth provider, then headers from SetHeader/AddHeader.
// The runtime resolves these below any per-request contributors.
func (b *BuilderCore[Self]) intrinsicHeaderValues() []requestValue[http.Header] {
	var values []requestValue[http.Header]
	if b.authHeader != nil {
		values = append(values, authValue{provider: b.authHeader})
	}
	return append(values, b.headerValues...)
}

// bakeMiddleware composes the client's intrinsic stack: inner middlewares
// (innermost), user outer middlewares, then telemetry (outermost). Telemetry is
// outermost so its panic recovery (which tags the error with the request span)
// and its metrics/span cover every user middleware. Auth and builder headers are
// applied separately as request-value contributors (see [intrinsicHeaderValues]).
// Refreshable behavior lives inside the middlewares and is read per request, so
// the result is static. Per-request middlewares are layered in below this by the runtime.
func (b *BuilderCore[Self]) bakeMiddleware() Middleware {
	var middlewares []Middleware
	middlewares = append(middlewares, b.innerMiddlewares...)
	middlewares = append(middlewares, b.middlewares...)
	middlewares = append(middlewares, &telemetryMiddleware{
		serviceName:         b.serviceName,
		disableMetrics:      b.disableMetrics,
		disableRecovery:     b.disableRecovery,
		disableRequestSpan:  b.disableRequestSpan,
		disableTraceHeaders: b.disableTraceHeaders,
		disableTraceMetrics: b.disableTraceMetrics,
		tags:                b.metricsTagProviders,
	})
	return composeMiddleware(middlewares...)
}

// BuildHTTPClient returns a refreshable *http.Client wrapping the raw transport
// in the intrinsic middleware stack (see [Builder.bakeMiddleware]) with the
// configured timeout. Unlike [Builder.Build], it performs no retries, URI
// scoring, or QoS redirect handling — it is the escape hatch for callers that
// want a plain *http.Client that still carries auth and telemetry.
func (b *BuilderCore[Self]) BuildHTTPClient(ctx context.Context) (refreshable.Refreshable[*http.Client], error) {
	transport, err := b.BuildTransport(ctx)
	if err != nil {
		return nil, err
	}
	if headerValues := b.intrinsicHeaderValues(); len(headerValues) > 0 {
		transport = wrapTransport(transport, decorationMiddleware{headerValues: headerValues})
	}
	transport = wrapTransport(transport, b.bakeMiddleware())
	mapped := refreshable.MapAuto(b.timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}
	})
	return mapped, nil
}

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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ServiceBuilder is an F-bounded interface for configuring service-level HTTP client
// settings: identity, base URLs, auth, headers, middleware, timeouts, retry, metrics,
// tracing, and error handling. It is the largest slice of [ClientBuilder]; see the
// package doc for the full hierarchy.
//
// Many settings support both static and refreshable setters (SetFoo / SetFooRefreshable):
// the refreshable variant updates at runtime without rebuilding the client.
// Clone preserves refreshable links (original and clone observe the same source);
// calling the static SetFoo replaces the link with a fixed value.
//
// The type parameter B is the concrete implementing type, so generic functions
// can configure any ServiceBuilder and return the same concrete type:
//
//	func ApplyDefaults[B ServiceBuilder[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	}
type ServiceBuilder[B ServiceBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	// Refreshable references are shared (both observe the same dynamic source).
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// Build constructs a Client from the current builder configuration.
	// The returned ConfigurableClient embeds Client and retains the builder
	// settings, allowing reconfiguration via client.Builder().
	// Build returns an error if required settings (e.g., base URLs) are missing.
	Build(ctx context.Context) (ConfigurableClient[B], error)

	// Identity

	// SetServiceName sets the logical service name used in metrics and logging.
	SetServiceName(string) B

	// SetServiceNameRefreshable sets a refreshable logical service name used in metrics and logging.
	SetServiceNameRefreshable(refreshable.Refreshable[string]) B

	// Base URLs

	// SetBaseURLs sets the static list of base URLs for the service.
	SetBaseURLs(...string) B

	// SetBaseURLsRefreshable sets a refreshable list of base URLs, enabling dynamic
	// service discovery updates.
	SetBaseURLsRefreshable(refreshable.Refreshable[[]string]) B

	// SetAllowCreateWithEmptyURIs allows building a client with no base URIs configured.
	// By default, Build fails if no URIs are set.
	SetAllowCreateWithEmptyURIs(bool) B

	// SetURIScoringStrategy sets the strategy for selecting among multiple base URIs.
	SetURIScoringStrategy(URIScoringStrategy) B

	// Auth

	// SetAuthToken sets a static bearer token for request authentication.
	SetAuthToken(string) B

	// SetAuthTokenProvider sets a function that provides a bearer token per-request.
	SetAuthTokenProvider(TokenProvider) B

	// SetAuthTokenRefreshable sets a refreshable bearer token for request authentication.
	// A nil *string disables auth; a non-nil *string sets the token.
	SetAuthTokenRefreshable(refreshable.Refreshable[*string]) B

	// SetBasicAuth sets static basic auth credentials.
	SetBasicAuth(user, password string) B

	// SetBasicAuthProvider sets a function that provides basic auth credentials per-request.
	SetBasicAuthProvider(BasicAuthProvider) B

	// SetBasicAuthRefreshable sets refreshable basic auth credentials.
	// A nil *BasicAuth disables auth; a non-nil *BasicAuth sets the credentials.
	SetBasicAuthRefreshable(refreshable.Refreshable[*BasicAuth]) B

	// Headers

	// AddHeader appends a header value. Multiple values for the same key are allowed.
	AddHeader(key, value string) B

	// SetHeader sets a header value, replacing any existing values for the key.
	SetHeader(key, value string) B

	// SetUserAgent sets the User-Agent header.
	SetUserAgent(string) B

	// SetOverrideRequestHost overrides the Host header on all requests.
	SetOverrideRequestHost(string) B

	// Middleware

	// AddMiddleware appends a middleware that wraps all previously added middleware.
	// The last-added middleware is outermost (sees the request first, response last).
	AddMiddleware(Middleware) B

	// AddInnerMiddleware prepends an inner middleware that runs closest to the transport.
	// The last-added inner middleware is innermost (sees the request last, response first).
	AddInnerMiddleware(Middleware) B

	// Timeout

	// SetTimeout sets the per-request timeout including retries.
	// Default: 60s.
	SetTimeout(time.Duration) B

	// SetTimeoutRefreshable sets a refreshable per-request timeout.
	SetTimeoutRefreshable(refreshable.Refreshable[time.Duration]) B

	// Retry

	// SetMaxAttempts sets the maximum number of total attempts (initial + retries).
	// Pass nil to use the default (2 attempts per base URL). A non-nil pointer
	// to 0 means unlimited attempts.
	SetMaxAttempts(*int) B

	// SetMaxAttemptsRefreshable sets a refreshable maximum number of total attempts.
	// nil uses the default (2 attempts per base URL); a non-nil *int of 0 means
	// unlimited attempts.
	SetMaxAttemptsRefreshable(refreshable.Refreshable[*int]) B

	// SetInitialBackoff sets the initial retry backoff duration.
	// Default: 250ms.
	SetInitialBackoff(time.Duration) B

	// SetInitialBackoffRefreshable sets a refreshable initial retry backoff duration.
	SetInitialBackoffRefreshable(refreshable.Refreshable[time.Duration]) B

	// SetMaxBackoff sets the maximum retry backoff duration.
	// Default: 2s.
	SetMaxBackoff(time.Duration) B

	// SetMaxBackoffRefreshable sets a refreshable maximum retry backoff duration.
	SetMaxBackoffRefreshable(refreshable.Refreshable[time.Duration]) B

	// Metrics

	// SetMetrics enables request metrics with the given tags providers.
	SetMetrics(...TagsProvider) B

	// SetDisableMetrics controls whether request metrics collection is disabled.
	SetDisableMetrics(bool) B

	// SetDisableMetricsRefreshable sets a refreshable toggle for disabling metrics collection.
	SetDisableMetricsRefreshable(refreshable.Refreshable[bool]) B

	// Tracing

	// DisableTracing disables distributed tracing instrumentation.
	DisableTracing() B

	// DisableTraceHeaderPropagation disables propagation of trace context headers.
	DisableTraceHeaderPropagation() B

	// Error handling

	// SetErrorDecoder sets a custom error decoder for responses.
	SetErrorDecoder(ErrorDecoder) B

	// DisableRestErrors disables the default REST error decoder.
	DisableRestErrors() B

	// Recovery

	// DisablePanicRecovery disables recovery from panics in middleware.
	DisablePanicRecovery() B

	// Transport injection (for standalone use without ClientBuilder)

	// SetTransport sets the underlying http.RoundTripper directly.
	// This is used when ServiceBuilder is used standalone without ClientBuilder,
	// or when ClientBuilder injects a fully-configured transport.
	SetTransport(http.RoundTripper) B

	// Misc

	// SetBytesBufferPool sets the buffer pool for request/response body buffering.
	SetBytesBufferPool(bytesbuffers.Pool) B
}

// SetServiceName sets the logical service name used in metrics tags and log fields.
func (b *Builder) SetServiceName(s string) *Builder {
	b.serviceName = refreshable.New(s)
	return b
}

// SetServiceNameRefreshable sets a refreshable logical service name.
func (b *Builder) SetServiceNameRefreshable(r refreshable.Refreshable[string]) *Builder {
	b.serviceName = r
	return b
}

// SetBaseURLs sets the static list of base URLs for the service. Each request is
// prefixed with one of these URLs (chosen by the URI scoring strategy).
func (b *Builder) SetBaseURLs(urls ...string) *Builder {
	b.uris = refreshable.New(urls)
	return b
}

// SetBaseURLsRefreshable sets a refreshable list of base URLs, enabling dynamic
// service discovery updates without rebuilding the client.
func (b *Builder) SetBaseURLsRefreshable(r refreshable.Refreshable[[]string]) *Builder {
	b.uris = r
	return b
}

// SetAllowCreateWithEmptyURIs allows building a client with no base URIs configured.
// By default, Build fails if no URIs are set. Requests against an empty-URI client
// fail with ErrEmptyURIs.
func (b *Builder) SetAllowCreateWithEmptyURIs(allow bool) *Builder {
	b.allowEmptyURIs = allow
	return b
}

// SetURIScoringStrategy sets the strategy for selecting among multiple base URIs.
// URIScoringBalanced (the default) routes away from slow or erroring hosts;
// URIScoringRandom selects uniformly at random.
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

// authHeaderFunc returns the Authorization header value for a request, or
// empty to skip setting the header. ctx is the request's context.
type authHeaderFunc func(ctx context.Context) (string, error)

// authHeaderMiddleware wraps a provider with the standard Authorization-header
// dance: skip if the header is already set, skip if the provider returns empty,
// propagate provider errors.
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

// SetAuthToken sets a static bearer token for request authentication.
// The token is sent as "Authorization: Bearer <token>" on every request unless
// the Authorization header is already set on the request.
func (b *Builder) SetAuthToken(t string) *Builder {
	b.authHeader = func(context.Context) (string, error) {
		return bearerAuthHeader(t), nil
	}
	return b
}

// SetAuthTokenProvider sets a function that provides a bearer token per-request.
// The provider is called once per request; an error is returned to the caller.
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

// SetAuthTokenRefreshable sets a refreshable bearer token. A nil *string disables auth
// (no Authorization header is sent); a non-nil *string supplies the token value.
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

// SetBasicAuthProvider sets a function that provides basic auth credentials per-request.
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

// SetBasicAuthRefreshable sets refreshable basic auth credentials. A nil *BasicAuth
// disables auth; a non-nil *BasicAuth supplies the credentials.
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

// AddHeader appends a header value to every request. Multiple values for the same key
// are allowed. For per-request headers, use Overrides.AddHeader or Endpoint.AddHeader.
func (b *Builder) AddHeader(key, value string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Add(key, value)
		return next.RoundTrip(req)
	}))
}

// SetHeader sets a header value on every request, replacing any existing values.
// For per-request headers, use Overrides.SetHeader or Endpoint.SetHeader.
func (b *Builder) SetHeader(key, value string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set(key, value)
		return next.RoundTrip(req)
	}))
}

// SetUserAgent sets the User-Agent header on every request.
func (b *Builder) SetUserAgent(s string) *Builder {
	return b.SetHeader("User-Agent", s)
}

// SetOverrideRequestHost overrides the Host header on every request, decoupling
// it from the URL host. Useful for virtual-host routing.
func (b *Builder) SetOverrideRequestHost(host string) *Builder {
	return b.AddInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Host = host
		return next.RoundTrip(req)
	}))
}

// AddMiddleware appends a middleware that wraps all previously added middleware.
// The last-added middleware is outermost: it sees the request first and the response last.
func (b *Builder) AddMiddleware(m Middleware) *Builder {
	b.middlewares = append(b.middlewares, m)
	return b
}

// AddInnerMiddleware prepends an inner middleware that runs closest to the transport,
// inside metrics and tracing. The last-added inner middleware is innermost.
func (b *Builder) AddInnerMiddleware(m Middleware) *Builder {
	b.innerMiddlewares = append([]Middleware{m}, b.innerMiddlewares...)
	return b
}

// SetTimeout sets the per-attempt timeout (applied to *http.Client.Timeout, so it
// covers the entire request including reading the body). Default: 60s.
func (b *Builder) SetTimeout(d time.Duration) *Builder {
	b.timeout = refreshable.New(d)
	return b
}

// SetTimeoutRefreshable sets a refreshable per-attempt timeout.
func (b *Builder) SetTimeoutRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.timeout = r
	return b
}

// SetMaxAttempts sets the maximum number of total attempts (initial + retries).
// Pass nil to use the default (2 attempts per base URL). A non-nil pointer to 0
// means unlimited attempts; n > 0 means exactly n total attempts.
func (b *Builder) SetMaxAttempts(p *int) *Builder {
	b.maxAttempts = refreshable.New(p)
	return b
}

// SetMaxAttemptsRefreshable sets a refreshable maximum number of total attempts.
// See SetMaxAttempts for the value semantics.
func (b *Builder) SetMaxAttemptsRefreshable(r refreshable.Refreshable[*int]) *Builder {
	b.maxAttempts = r
	return b
}

// SetInitialBackoff sets the initial retry backoff duration. Default: 250ms.
func (b *Builder) SetInitialBackoff(d time.Duration) *Builder {
	b.initialBackoff = refreshable.New(d)
	return b
}

// SetInitialBackoffRefreshable sets a refreshable initial retry backoff duration.
func (b *Builder) SetInitialBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.initialBackoff = r
	return b
}

// SetMaxBackoff sets the maximum retry backoff duration. Default: 2s.
func (b *Builder) SetMaxBackoff(d time.Duration) *Builder {
	b.maxBackoff = refreshable.New(d)
	return b
}

// SetMaxBackoffRefreshable sets a refreshable maximum retry backoff duration.
func (b *Builder) SetMaxBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *Builder {
	b.maxBackoff = r
	return b
}

// SetMetrics enables request metrics with the given additional tag providers.
// The built-in service-name, method, status-family, and RPC-method tags are
// always emitted; providers add further tags. See README.md for the full
// metrics catalog.
func (b *Builder) SetMetrics(providers ...TagsProvider) *Builder {
	b.disableMetrics = refreshable.New(false)
	b.metricsTagProviders = providers
	return b
}

// SetDisableMetrics disables request metrics collection.
func (b *Builder) SetDisableMetrics(disable bool) *Builder {
	b.disableMetrics = refreshable.New(disable)
	return b
}

// SetDisableMetricsRefreshable sets a refreshable toggle for disabling metrics.
func (b *Builder) SetDisableMetricsRefreshable(r refreshable.Refreshable[bool]) *Builder {
	b.disableMetrics = r
	return b
}

// DisableTracing disables creation of per-request tracing spans.
// Trace header propagation is controlled separately via DisableTraceHeaderPropagation.
func (b *Builder) DisableTracing() *Builder {
	b.disableRequestSpan = true
	return b
}

// DisableTraceHeaderPropagation disables propagation of B3 trace headers
// (X-B3-TraceId, etc.) to downstream services.
func (b *Builder) DisableTraceHeaderPropagation() *Builder {
	b.disableTraceHeaders = true
	return b
}

// SetErrorDecoder sets a custom error decoder for responses. Replaces the
// default decoder (which handles status >= 307); pass nil or call
// DisableRestErrors to disable error decoding entirely.
func (b *Builder) SetErrorDecoder(d ErrorDecoder) *Builder {
	b.errorDecoder = d
	return b
}

// DisableRestErrors disables the default REST error decoder. With this set,
// all responses (including 4xx/5xx) are returned to the caller as successful
// (resp, nil); the caller is responsible for inspecting StatusCode.
func (b *Builder) DisableRestErrors() *Builder {
	b.errorDecoder = nil
	return b
}

// DisablePanicRecovery disables panic recovery in the middleware chain.
// By default, a panic in middleware or transport is recovered and returned as an error.
func (b *Builder) DisablePanicRecovery() *Builder {
	b.disableRecovery = true
	return b
}

// SetTransport injects a fully-configured http.RoundTripper, bypassing the
// dialer, TLS, and transport builders. Useful for tests or for wrapping a
// custom transport. The injected transport is still wrapped with the
// middleware stack (auth, metrics, tracing, recovery, URI scoring, retries).
func (b *Builder) SetTransport(rt http.RoundTripper) *Builder {
	b.transport = rt
	return b
}

// SetBytesBufferPool sets a buffer pool used by codecs (notably JSONEncoder) to
// reduce per-request allocations.
func (b *Builder) SetBytesBufferPool(pool bytesbuffers.Pool) *Builder {
	b.bytesBufferPool = pool
	return b
}

// Build constructs a [ConfigurableClient] from the current builder configuration.
// Returns an error if required settings are missing (no base URLs, unless
// SetAllowCreateWithEmptyURIs(true) was called) or if any deferred validation
// errors accumulated during setter calls (e.g., invalid proxy URLs).
func (b *Builder) Build(ctx context.Context) (ConfigurableClient[*Builder], error) {
	if err := b.buildError(ctx); err != nil {
		return nil, err
	}
	if b.uris == nil {
		return nil, werror.ErrorWithContextParams(ctx, "httpclient URLs must be set in configuration or by constructor param", werror.SafeParam("serviceName", b.serviceName.Current()))
	}
	if !b.allowEmptyURIs && len(b.uris.Current()) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs{}, "", werror.SafeParam("serviceName", b.serviceName.Current()))
	}

	// Build the http.Client.
	httpClient, err := b.BuildHTTPClient(ctx)
	if err != nil {
		return nil, err
	}

	// Recovery middleware.
	var recovery Middleware
	if !b.disableRecovery {
		recovery = recoveryMiddleware{}
	}

	// URI scorer.
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
			errorDecoder:   b.errorDecoder,
			recoveryMW:     recovery,
			uriScorer:      uriScorer,
			maxAttempts:    b.maxAttempts,
			initialBackoff: b.initialBackoff,
			maxBackoff:     b.maxBackoff,
			bufferPool:     b.bytesBufferPool,
		},
		builder: b.Clone(),
	}, nil
}

// BuildHTTPClient builds a complete *http.Client from the builder's configuration.
// The returned refreshable rebuilds whenever timeout or transport settings change.
// The transport is wrapped with auth, inner middlewares, metrics, and tracing.
// Panic recovery is NOT included here; Build adds a single recovery layer in doOnce
// that covers the entire middleware chain (including user outer middleware).
func (b *Builder) BuildHTTPClient(ctx context.Context) (refreshable.Refreshable[*http.Client], error) {
	transport, err := b.BuildTransport(ctx)
	if err != nil {
		return nil, err
	}

	// Wrap with auth middleware (innermost, closest to transport).
	if b.authHeader != nil {
		transport = wrapTransport(transport, authHeaderMiddleware(b.authHeader))
	}
	// Wrap with inner middlewares.
	transport = wrapTransport(transport, b.innerMiddlewares...)
	// Metrics middleware.
	transport = wrapTransport(transport, &metricsMiddleware{
		disabled:    b.disableMetrics,
		serviceName: b.serviceName,
		tags:        b.metricsTagProviders,
	})
	// Tracing middleware.
	transport = wrapTransport(transport, &traceMiddleware{
		serviceName:         b.serviceName,
		disableRequestSpan:  b.disableRequestSpan,
		disableTraceHeaders: b.disableTraceHeaders,
	})
	mapped := refreshable.MapAuto(b.timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}
	})
	return mapped, nil
}

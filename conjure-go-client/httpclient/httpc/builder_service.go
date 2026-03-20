package httpc

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// ServiceBuilder is an F-bounded interface for configuring service-level HTTP client settings.
// It covers identity, base URLs, authentication, headers, middleware, timeouts, retry,
// metrics, tracing, and error handling.
//
// Like all builders in this package, ServiceBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Many settings support dual setters: SetFoo(T) for static values and
// SetFooRefreshable(Refreshable[T]) for values that update at runtime without
// rebuilding the client. Clone preserves refreshable links (both the original and
// the clone observe the same dynamic source). Calling the static SetFoo overrides
// the refreshable link with a fixed value.
//
// Generic functions can accept any ServiceBuilder and return the same concrete type:
//
//	func ApplyDefaults[B ServiceBuilder[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxRetries(3)
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

	// AddMiddleware appends an outer middleware that wraps all previously added middleware.
	AddMiddleware(Middleware) B

	// AddInnerMiddleware appends an inner middleware that runs closest to the transport.
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

func (b *StandardClientBuilder) SetServiceName(s string) *StandardClientBuilder {
	b.serviceName = refreshable.New(s)
	return b
}

func (b *StandardClientBuilder) SetServiceNameRefreshable(r refreshable.Refreshable[string]) *StandardClientBuilder {
	b.serviceName = r
	return b
}

func (b *StandardClientBuilder) SetBaseURLs(urls ...string) *StandardClientBuilder {
	b.uris = refreshable.New(urls)
	return b
}

func (b *StandardClientBuilder) SetBaseURLsRefreshable(r refreshable.Refreshable[[]string]) *StandardClientBuilder {
	b.uris = r
	return b
}

func (b *StandardClientBuilder) SetAllowCreateWithEmptyURIs(allow bool) *StandardClientBuilder {
	b.allowEmptyURIs = allow
	return b
}

func (b *StandardClientBuilder) SetURIScoringStrategy(s URIScoringStrategy) *StandardClientBuilder {
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

func (b *StandardClientBuilder) SetAuthToken(t string) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if t != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t))
		}
		return next.RoundTrip(req)
	}))
	return b
}

func (b *StandardClientBuilder) SetAuthTokenProvider(p TokenProvider) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		token, err := p(req.Context())
		if err != nil {
			return nil, err
		}
		if token != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		}
		return next.RoundTrip(req)
	}))
	return b
}

func (b *StandardClientBuilder) SetAuthTokenRefreshable(r refreshable.Refreshable[*string]) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if s := r.Current(); s != nil && *s != "" {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", *s))
		}
		return next.RoundTrip(req)
	}))
	return b
}

func (b *StandardClientBuilder) SetBasicAuth(user, password string) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		setBasicAuthHeader(req.Header, user, password)
		return next.RoundTrip(req)
	}))
	return b
}

func (b *StandardClientBuilder) SetBasicAuthProvider(p BasicAuthProvider) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		auth, err := p(req.Context())
		if err != nil {
			return nil, err
		}
		setBasicAuthHeader(req.Header, auth.User, auth.Password)
		return next.RoundTrip(req)
	}))
	return b
}

func (b *StandardClientBuilder) SetBasicAuthRefreshable(r refreshable.Refreshable[*BasicAuth]) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if auth := r.Current(); auth != nil {
			setBasicAuthHeader(req.Header, auth.User, auth.Password)
		}
		return next.RoundTrip(req)
	}))
	return b
}

func setBasicAuthHeader(h http.Header, username, password string) {
	basicAuthBytes := []byte(username + ":" + password)
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString(basicAuthBytes))
}

func (b *StandardClientBuilder) AddHeader(key, value string) *StandardClientBuilder {
	b.middlewares = append([]Middleware{MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Add(key, value)
		return next.RoundTrip(req)
	})}, b.middlewares...)
	return b
}

func (b *StandardClientBuilder) SetHeader(key, value string) *StandardClientBuilder {
	b.middlewares = append([]Middleware{MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set(key, value)
		return next.RoundTrip(req)
	})}, b.middlewares...)
	return b
}

func (b *StandardClientBuilder) SetUserAgent(s string) *StandardClientBuilder {
	return b.SetHeader("User-Agent", s)
}

func (b *StandardClientBuilder) SetOverrideRequestHost(host string) *StandardClientBuilder {
	b.middlewares = append([]Middleware{MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Host = host
		return next.RoundTrip(req)
	})}, b.middlewares...)
	return b
}

func (b *StandardClientBuilder) AddMiddleware(m Middleware) *StandardClientBuilder {
	b.middlewares = append(b.middlewares, m)
	return b
}

func (b *StandardClientBuilder) AddInnerMiddleware(m Middleware) *StandardClientBuilder {
	b.innerMiddlewares = append(b.innerMiddlewares, m)
	return b
}

func (b *StandardClientBuilder) SetTimeout(d time.Duration) *StandardClientBuilder {
	b.timeout = refreshable.New(d)
	return b
}

func (b *StandardClientBuilder) SetTimeoutRefreshable(r refreshable.Refreshable[time.Duration]) *StandardClientBuilder {
	b.timeout = r
	return b
}

func (b *StandardClientBuilder) SetMaxAttempts(p *int) *StandardClientBuilder {
	b.maxAttempts = refreshable.New(p)
	return b
}

func (b *StandardClientBuilder) SetMaxAttemptsRefreshable(r refreshable.Refreshable[*int]) *StandardClientBuilder {
	b.maxAttempts = r
	return b
}

func (b *StandardClientBuilder) SetInitialBackoff(d time.Duration) *StandardClientBuilder {
	b.initialBackoff = refreshable.New(d)
	return b
}

func (b *StandardClientBuilder) SetInitialBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *StandardClientBuilder {
	b.initialBackoff = r
	return b
}

func (b *StandardClientBuilder) SetMaxBackoff(d time.Duration) *StandardClientBuilder {
	b.maxBackoff = refreshable.New(d)
	return b
}

func (b *StandardClientBuilder) SetMaxBackoffRefreshable(r refreshable.Refreshable[time.Duration]) *StandardClientBuilder {
	b.maxBackoff = r
	return b
}

func (b *StandardClientBuilder) SetMetrics(providers ...TagsProvider) *StandardClientBuilder {
	b.disableMetrics = refreshable.New(false)
	b.metricsTagProviders = providers
	return b
}

func (b *StandardClientBuilder) SetDisableMetrics(disable bool) *StandardClientBuilder {
	b.disableMetrics = refreshable.New(disable)
	return b
}

func (b *StandardClientBuilder) SetDisableMetricsRefreshable(r refreshable.Refreshable[bool]) *StandardClientBuilder {
	b.disableMetrics = r
	return b
}

func (b *StandardClientBuilder) DisableTracing() *StandardClientBuilder {
	b.disableRequestSpan = true
	return b
}

func (b *StandardClientBuilder) DisableTraceHeaderPropagation() *StandardClientBuilder {
	b.disableTraceHeaders = true
	return b
}

func (b *StandardClientBuilder) SetErrorDecoder(d ErrorDecoder) *StandardClientBuilder {
	b.errorDecoder = d
	return b
}

func (b *StandardClientBuilder) DisableRestErrors() *StandardClientBuilder {
	b.errorDecoder = nil
	return b
}

func (b *StandardClientBuilder) DisablePanicRecovery() *StandardClientBuilder {
	b.disableRecovery = true
	return b
}

func (b *StandardClientBuilder) SetTransport(rt http.RoundTripper) *StandardClientBuilder {
	b.transport = rt
	return b
}

func (b *StandardClientBuilder) SetBytesBufferPool(pool bytesbuffers.Pool) *StandardClientBuilder {
	b.bytesBufferPool = pool
	return b
}

// Build constructs a Client from the current builder configuration.
func (b *StandardClientBuilder) Build(ctx context.Context) (ConfigurableClient[*StandardClientBuilder], error) {
	if len(b.errs) > 0 {
		if len(b.errs) == 1 {
			return nil, werror.WrapWithContextParams(ctx, b.errs[0], "builder configuration errors")
		}
		return nil, werror.WrapWithContextParams(ctx, errors.Join(b.errs...), "builder configuration errors")
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

	// Error decoder middleware.
	var edm Middleware
	if b.errorDecoder != nil {
		edm = errorDecoderMiddleware{decoder: b.errorDecoder}
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

	return &fluentClient[*StandardClientBuilder]{
		serviceName:    b.serviceName,
		httpClient:     httpClient,
		middlewares:    b.middlewares,
		errorDecoderMW: edm,
		recoveryMW:     recovery,
		uriScorer:      uriScorer,
		maxAttempts:    b.maxAttempts,
		initialBackoff: b.initialBackoff,
		maxBackoff:     b.maxBackoff,
		bufferPool:     b.bytesBufferPool,
		builder:        b.Clone(),
	}, nil
}

// BuildHTTPClient builds a complete *http.Client from the builder's configuration.
// The returned refreshable rebuilds whenever timeout or transport settings change.
// The transport is wrapped with inner middlewares, metrics, tracing, and recovery.
func (b *StandardClientBuilder) BuildHTTPClient(ctx context.Context) (refreshable.Refreshable[*http.Client], error) {
	transport, err := b.BuildTransport(ctx)
	if err != nil {
		return nil, err
	}

	// Wrap with inner middlewares (closest to transport).
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
	// Inner recovery (before user middleware, catches panics with trace context).
	if !b.disableRecovery {
		transport = wrapTransport(transport, recoveryMiddleware{})
	}
	mapped, _ := refreshable.Map(b.timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout:   timeout,
			Transport: transport,
		}
	})
	return mapped, nil
}

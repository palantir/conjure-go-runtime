package httpc

import (
	"context"
	"net/http"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
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
//	func ApplyDefaults[Self ServiceBuilder[Self]](b Self) Self {
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

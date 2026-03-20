package httpc

import (
	"bytes"
	"crypto/tls"
	"net/http"
	"slices"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/refreshable/v2"
)

// baseBuilder is the shared contract for all builder interfaces in this package.
//
// Builders are mutable: setter methods modify the receiver and return it for fluent
// chaining. Clone returns an independent deep copy for forking a configuration;
// mutations to the clone do not affect the original, and vice versa.
//
// Apply applies Param functions to the builder in sequence. Because builders are
// mutable, Apply modifies the receiver in place and returns it. This means that
// after b2 := b1.Apply(p), b1 and b2 refer to the same (now-modified) builder.
// To create a genuinely independent variant, clone first: b2 := b1.Clone().Apply(p).
type baseBuilder[B baseBuilder[B]] interface {
	Clone() B
	Apply(...Param[B]) B
}

// Param is a reusable, composable configuration function for a builder or service client.
// Params are applied via the Apply method and typically wrap one or more setter calls:
//
//	func WithDefaults[B httpc.ClientBuilder[B]]() httpc.Param[B] {
//	    return func(b B) B {
//	        return b.SetTimeout(30 * time.Second).SetMaxRetries(3)
//	    }
//	}
//
// Param is also the option type for ServiceClient, where it wraps copy-on-write
// RequestOverrides methods rather than mutating setters. See ServiceClient for details.
type Param[Self baseBuilder[Self]] func(Self) Self

// ClientBuilder is the top-level builder interface for constructing HTTP clients.
// It composes DialerBuilder, TLSConfigBuilder, TransportBuilder, and ServiceBuilder
// into a single unified builder covering all layers of the HTTP stack.
//
// Like all builders in this package, ClientBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// The Build method (inherited from ServiceBuilder) constructs the client by:
//  1. Building a net.Dialer from DialerBuilder settings
//  2. Building a *tls.Config from TLSConfigBuilder settings
//  3. Building an *http.Transport from TransportBuilder settings + dialer + TLS
//  4. Injecting the transport via SetTransport
//  5. Wrapping with the middleware stack and returning a Client
//
// Generic configuration functions can accept any ClientBuilder and return the
// same concrete type, enabling reusable configuration libraries:
//
//	func ApplyDefaults[B ClientBuilder[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxRetries(3)
//	}
type ClientBuilder[B ClientBuilder[B]] interface {
	DialerBuilder[B]
	TLSConfigBuilder[B]
	TransportBuilder[B]
	ServiceBuilder[B]
}

const (
	defaultDialTimeout           = 10 * time.Second
	defaultHTTPTimeout           = 60 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
	defaultHTTP2ReadIdleTimeout  = 30 * time.Second
	defaultHTTP2PingTimeout      = 15 * time.Second
	defaultInitialBackoff        = 250 * time.Millisecond
	defaultMaxBackoff            = 2 * time.Second
)

// StandardClientBuilder implements ClientBuilder by directly managing
// transport, dialer, TLS, and service-level configuration.
type StandardClientBuilder struct {
	serviceName     string
	timeout         refreshable.Refreshable[time.Duration]
	dialerParams    refreshable.Refreshable[dialerParams]
	tlsConfig       *tls.Config // escape hatch: replaces all other TLS settings
	transportParams refreshable.Refreshable[transportParams]
	tlsFileParams   refreshable.Refreshable[tlsFileParams] // TLS file/flag settings
	tlsCABytes      refreshable.Refreshable[[][]byte]

	middlewares      []Middleware // outer: applied after built-in middleware
	innerMiddlewares []Middleware // inner: applied before built-in middleware

	disableMetrics      refreshable.Refreshable[bool]
	metricsTagProviders []TagsProvider
	disableRequestSpan  bool
	disableRecovery     bool
	disableTraceHeaders bool

	uris             refreshable.Refreshable[[]string]
	uriScorerBuilder func([]string) internal.URIScoringMiddleware
	allowEmptyURIs   bool

	errorDecoder    ErrorDecoder
	bytesBufferPool bytesbuffers.Pool
	maxAttempts     refreshable.Refreshable[*int]
	initialBackoff  refreshable.Refreshable[time.Duration]
	maxBackoff      refreshable.Refreshable[time.Duration]

	transport        http.RoundTripper // escape hatch: direct transport injection
	caByteSlices     [][]byte          // static CA cert bytes from AddCACertBytes
	clientCertKey    []byte            // client cert key bytes
	clientCertCert   []byte            // client cert bytes
	includeSystemCAs bool
	errs             []error
}

// Compile-time interface check.
var _ ClientBuilder[*StandardClientBuilder] = (*StandardClientBuilder)(nil)

// NewStandardClientBuilder creates a new StandardClientBuilder with sane defaults.
func NewStandardClientBuilder() *StandardClientBuilder {
	return &StandardClientBuilder{
		serviceName: "",
		timeout:     refreshable.New(defaultHTTPTimeout),
		dialerParams: refreshable.New(dialerParams{
			DialTimeout: defaultDialTimeout,
			KeepAlive:   defaultKeepAlive,
		}),
		transportParams: refreshable.New(transportParams{
			MaxIdleConns:          defaultMaxIdleConns,
			MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
			IdleConnTimeout:       defaultIdleConnTimeout,
			ExpectContinueTimeout: defaultExpectContinueTimeout,
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ProxyFromEnvironment:  true,
			HTTP2ReadIdleTimeout:  defaultHTTP2ReadIdleTimeout,
			HTTP2PingTimeout:      defaultHTTP2PingTimeout,
		}),
		tlsFileParams:  refreshable.New(tlsFileParams{}),
		disableMetrics: refreshable.New(false),
		errorDecoder:   defaultRestErrorDecoder{},
		initialBackoff: refreshable.New(defaultInitialBackoff),
		maxBackoff:     refreshable.New(defaultMaxBackoff),
	}
}

// Clone returns a deep copy of the builder.
func (b *StandardClientBuilder) Clone() *StandardClientBuilder {
	var clonedTLSConfig *tls.Config
	if b.tlsConfig != nil {
		clonedTLSConfig = b.tlsConfig.Clone()
	}
	clone := &StandardClientBuilder{
		serviceName:         b.serviceName,
		timeout:             b.timeout,
		dialerParams:        b.dialerParams,
		transportParams:     b.transportParams,
		tlsFileParams:       b.tlsFileParams,
		tlsConfig:           clonedTLSConfig,
		tlsCABytes:          b.tlsCABytes,
		middlewares:         slices.Clone(b.middlewares),
		innerMiddlewares:    slices.Clone(b.innerMiddlewares),
		disableMetrics:      b.disableMetrics,
		metricsTagProviders: slices.Clone(b.metricsTagProviders),
		disableRequestSpan:  b.disableRequestSpan,
		disableRecovery:     b.disableRecovery,
		disableTraceHeaders: b.disableTraceHeaders,
		uris:                b.uris,
		uriScorerBuilder:    b.uriScorerBuilder,
		allowEmptyURIs:      b.allowEmptyURIs,
		errorDecoder:        b.errorDecoder,
		bytesBufferPool:     b.bytesBufferPool,
		maxAttempts:         b.maxAttempts,
		initialBackoff:      b.initialBackoff,
		maxBackoff:          b.maxBackoff,
		transport:           b.transport,
		caByteSlices:        slices.Clone(b.caByteSlices),
		clientCertKey:       bytes.Clone(b.clientCertKey),
		clientCertCert:      bytes.Clone(b.clientCertCert),
		includeSystemCAs:    b.includeSystemCAs,
		errs:                slices.Clone(b.errs),
	}
	return clone
}

// Apply applies the given Param functions to the builder in sequence.
func (b *StandardClientBuilder) Apply(params ...Param[*StandardClientBuilder]) *StandardClientBuilder {
	for _, p := range params {
		p(b)
	}
	return b
}

// StatusCodeFromError retrieves the 'statusCode' parameter from the provided error.
// If the error is not a werror or does not have the statusCode param, ok is false.
//
// The default client error decoder sets the statusCode parameter on its returned errors.
// Note that, if a custom error decoder is used, this function will only return a status
// code for the error if the custom decoder sets a 'statusCode' parameter on the error.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	return internal.StatusCodeFromError(err)
}

// LocationFromError retrieves the 'location' parameter from the provided error.
// If the error is not a werror or does not have the location param, ok is false.
//
// The default client error decoder sets the location parameter on its returned errors
// if the status code is 3xx and a location is set in the response header.
func LocationFromError(err error) (location string, ok bool) {
	return internal.LocationFromError(err)
}

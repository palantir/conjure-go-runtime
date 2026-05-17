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
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// baseB is the shared contract for all builder interfaces in this package.
//
// Builders are mutable: setter methods modify the receiver and return it for fluent
// chaining. Clone returns an independent deep copy for forking a configuration;
// mutations to the clone do not affect the original, and vice versa.
//
// Apply applies Param functions to the builder in sequence. Because builders are
// mutable, Apply modifies the receiver in place and returns it. This means that
// after b2 := b1.Apply(p), b1 and b2 refer to the same (now-modified) builder.
// To create a genuinely independent variant, clone first: b2 := b1.Clone().Apply(p).
type baseB[B baseB[B]] interface {
	Clone() B
	Apply(...Param[B]) B
}

// Param is a reusable, composable configuration function for a builder or service client.
// Params are applied via the Apply method and typically wrap one or more setter calls:
//
//	func WithDefaults[B httpc.ServiceBuilder[B]]() httpc.Param[B] {
//	    return func(b B) B {
//	        return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	    }
//	}
//
// Param is also the option type for Overrides, where it wraps copy-on-write
// RequestOverrides methods rather than mutating setters. See Overrides for details.
type Param[B baseB[B]] func(B) B

// Param0 wraps a zero-argument builder method as a Param.
// Example: Param0((*Builder).DisableHTTP2).
func Param0[B baseB[B]](p func(B) B) Param[B] {
	return func(b B) B {
		return p(b)
	}
}

// Param1 wraps a one-argument builder method and its argument as a Param.
// Example: Param1((*Builder).SetTimeout, 30*time.Second).
func Param1[B baseB[B], X any](p func(B, X) B, x X) Param[B] {
	return func(b B) B {
		return p(b, x)
	}
}

// Param2 wraps a two-argument builder method and its arguments as a Param.
// Example: Param2((*Builder).SetBasicAuth, "user", "pass").
func Param2[B baseB[B], X any, Y any](p func(B, X, Y) B, x X, y Y) Param[B] {
	return func(b B) B { return p(b, x, y) }
}

// ParamVarArgs wraps a variadic builder method and a slice of arguments as a Param.
// Example: ParamVarArgs((*Builder).SetBaseURLs, []string{"https://a", "https://b"}).
func ParamVarArgs[B baseB[B], X any](p func(B, ...X) B, xs []X) Param[B] {
	return func(b B) B { return p(b, xs...) }
}

// ClientBuilder is the top-level builder interface for constructing HTTP clients.
// It composes [DialerBuilder], [TLSConfigBuilder], [TransportBuilder], and
// [ServiceBuilder] into a single unified builder covering all layers of the
// HTTP stack. [Builder] is the concrete implementation; in application code
// you usually work with *Builder directly. The interface is provided so that
// generic helpers can configure any ClientBuilder and return the same concrete
// type:
//
//	func ApplyDefaults[B ClientBuilder[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	}
//
// Build (inherited from ServiceBuilder) constructs the client by building the
// dialer, TLS config, and transport from their respective settings, then
// wrapping the transport with the middleware stack.
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

// Builder is the concrete [ClientBuilder] returned by [NewBuilder]. It carries
// the full set of dialer, TLS, transport, and service-level settings, and
// exposes additional methods for building intermediate artifacts (BuildDialer,
// BuildTLSConfig, BuildTransport, BuildHTTPClient).
//
// Builder is NOT safe for concurrent use. All setter methods mutate the
// receiver and return it for fluent chaining; to share configuration across
// goroutines, call [Builder.Clone] before mutating in each goroutine.
type Builder struct {
	serviceName     refreshable.Refreshable[string]
	timeout         refreshable.Refreshable[time.Duration]
	dialerParams    refreshable.Refreshable[dialerParams]
	tlsConfig       *tls.Config // escape hatch: replaces all other TLS settings
	transportParams refreshable.Refreshable[transportParams]
	tlsFileParams   refreshable.Refreshable[tlsFileParams] // TLS file/flag settings
	tlsCABytes      refreshable.Refreshable[[][]byte]

	middlewares      []Middleware   // outer: applied after built-in middleware
	innerMiddlewares []Middleware   // inner: applied before built-in middleware
	authHeader       authHeaderFunc // single auth slot: replaced (not accumulated) by Set*Auth* methods

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

// buildError returns a combined error from the builder's accumulated errors, or nil.
func (b *Builder) buildError(ctx context.Context) error {
	switch len(b.errs) {
	case 0:
		return nil
	case 1:
		return werror.WrapWithContextParams(ctx, b.errs[0], "builder configuration errors")
	default:
		return werror.WrapWithContextParams(ctx, errors.Join(b.errs...), "builder configuration errors")
	}
}

// Compile-time interface check.
var _ ClientBuilder[*Builder] = (*Builder)(nil)

// NewBuilder creates a new Builder with sane defaults.
func NewBuilder() *Builder {
	return &Builder{
		serviceName: refreshable.New(""),
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
		tlsFileParams:    refreshable.New(tlsFileParams{}),
		disableMetrics:   refreshable.New(false),
		errorDecoder:     defaultRestErrorDecoder{},
		initialBackoff:   refreshable.New(defaultInitialBackoff),
		maxBackoff:       refreshable.New(defaultMaxBackoff),
		includeSystemCAs: true,
	}
}

// Clone returns a deep copy of the builder.
func (b *Builder) Clone() *Builder {
	var clonedTLSConfig *tls.Config
	if b.tlsConfig != nil {
		clonedTLSConfig = b.tlsConfig.Clone()
	}
	clone := &Builder{
		serviceName:         b.serviceName, // shared refreshable reference
		timeout:             b.timeout,
		dialerParams:        b.dialerParams,
		transportParams:     b.transportParams,
		tlsFileParams:       b.tlsFileParams,
		tlsConfig:           clonedTLSConfig,
		tlsCABytes:          b.tlsCABytes,
		middlewares:         slices.Clone(b.middlewares),
		innerMiddlewares:    slices.Clone(b.innerMiddlewares),
		authHeader:          b.authHeader,
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
func (b *Builder) Apply(params ...Param[*Builder]) *Builder {
	for _, p := range params {
		p(b)
	}
	return b
}

// ApplyConfig validates the config and calls builder setter methods for each
// field the config explicitly specifies. Fields not present in the config are
// left at whatever the builder already has (from NewBuilder
// defaults or prior setter calls), preserving composability.
// Validation errors are deferred to b.errs (surfaced at Build time).
func (b *Builder) ApplyConfig(config ClientConfig) *Builder {
	params, err := newValidatedClientParams(config)
	if err != nil {
		b.errs = append(b.errs, err)
		return b
	}
	if params.serviceName != "" {
		b.SetServiceName(params.serviceName)
	}
	if params.uris != nil {
		b.SetBaseURLs(params.uris...)
	}

	// Auth — only install the middleware the config specifies.
	if params.apiToken != nil {
		b.SetAuthToken(*params.apiToken)
	} else if params.basicAuth != nil {
		b.SetBasicAuth(params.basicAuth.User, params.basicAuth.Password)
	}

	if params.timeout != nil {
		b.SetTimeout(*params.timeout)
	}

	// Dialer fields.
	if params.connectTimeout != nil {
		b.SetDialTimeout(*params.connectTimeout)
	}
	if params.keepAlive != nil {
		b.SetKeepAlive(*params.keepAlive)
	}
	if params.socksProxyURL != nil {
		b.SetSocksProxyURL(params.socksProxyURL.String())
	}

	// Transport fields.
	if params.httpProxyURL != nil {
		b.SetHTTPProxyURL(params.httpProxyURL.String())
	}
	if params.proxyFromEnvironment != nil {
		if *params.proxyFromEnvironment {
			b.SetProxyFromEnvironment()
		} else {
			b.transportParams = refreshable.View(b.transportParams, func(tp transportParams) transportParams {
				tp.ProxyFromEnvironment = false
				return tp
			})
		}
	}
	if params.maxIdleConns != nil {
		b.SetMaxIdleConns(*params.maxIdleConns)
	}
	if params.maxIdleConnsPerHost != nil {
		b.SetMaxIdleConnsPerHost(*params.maxIdleConnsPerHost)
	}
	if params.disableHTTP2 != nil && *params.disableHTTP2 {
		b.DisableHTTP2()
	}
	if params.idleConnTimeout != nil {
		b.SetIdleConnTimeout(*params.idleConnTimeout)
	}
	if params.expectContinueTimeout != nil {
		b.SetExpectContinueTimeout(*params.expectContinueTimeout)
	}
	if params.responseHeaderTimeout != nil {
		b.SetResponseHeaderTimeout(*params.responseHeaderTimeout)
	}
	if params.tlsHandshakeTimeout != nil {
		b.SetTLSHandshakeTimeout(*params.tlsHandshakeTimeout)
	}
	if params.http2ReadIdleTimeout != nil {
		b.SetHTTP2ReadIdleTimeout(*params.http2ReadIdleTimeout)
	}
	if params.http2PingTimeout != nil {
		b.SetHTTP2PingTimeout(*params.http2PingTimeout)
	}

	// TLS
	if len(params.caFiles) > 0 {
		b.AddCACertFiles(params.caFiles...)
	}
	if params.certFile != "" && params.keyFile != "" {
		b.SetClientCertFiles(params.keyFile, params.certFile)
	}
	if params.insecureSkipVerify != nil {
		b.SetInsecureSkipVerify(*params.insecureSkipVerify)
	}
	if params.dynamicCertReload != nil {
		b.SetDynamicCertReload(*params.dynamicCertReload)
	}

	// Retry
	if params.maxAttempts != nil {
		b.SetMaxAttempts(params.maxAttempts)
	}
	if params.initialBackoff != nil {
		b.SetInitialBackoff(*params.initialBackoff)
	}
	if params.maxBackoff != nil {
		b.SetMaxBackoff(*params.maxBackoff)
	}

	// Metrics
	if params.disableMetrics != nil {
		b.SetDisableMetrics(*params.disableMetrics)
	}
	if len(params.metricsTags) > 0 {
		b.metricsTagProviders = append(b.metricsTagProviders, StaticTagsProvider(params.metricsTags))
	}
	return b
}

// ApplyConfigRefreshable validates the initial config and wires up refreshable
// overlays for each field the config specifies. When a config field is unset,
// the builder's existing value (captured at call time) is used as the fallback.
// Validation errors are deferred to b.errs (surfaced at Build time).
func (b *Builder) ApplyConfigRefreshable(ctx context.Context, config refreshable.Refreshable[ClientConfig]) *Builder {
	// Stage 1: Validate initial config and create validated refreshable.
	validParams, err := refreshable.MapWithErrorAuto(ctx, config, func(ctx context.Context, config ClientConfig) (validatedClientParams, error) {
		return newValidatedClientParams(config)
	})
	if err != nil {
		b.errs = append(b.errs, err)
		return b
	}

	// Stage 2: For each field, create a refreshable that overlays the config
	// value (when set) on top of the builder's existing value (captured now).

	existingServiceName := b.serviceName
	b.serviceName = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) string {
		if p.serviceName != "" {
			return p.serviceName
		}
		return existingServiceName.Current()
	})

	existingURIs := b.uris
	uris := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) []string {
		if p.uris != nil {
			return p.uris
		}
		if existingURIs != nil {
			return existingURIs.Current()
		}
		return nil
	})
	b.uris = uris

	existingTimeout := b.timeout
	b.timeout = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.timeout != nil {
			return *p.timeout
		}
		return existingTimeout.Current()
	})

	// Dialer: overlay individual config fields onto existing dialer params.
	existingDialer := b.dialerParams
	b.dialerParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) dialerParams {
		d := existingDialer.Current()
		if p.connectTimeout != nil {
			d.DialTimeout = *p.connectTimeout
		}
		if p.keepAlive != nil {
			d.KeepAlive = *p.keepAlive
		}
		if p.socksProxyURL != nil {
			d.SocksProxyURL = p.socksProxyURL
		}
		return d
	})

	// Transport: overlay individual config fields onto existing transport params.
	existingTransport := b.transportParams
	b.transportParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) transportParams {
		t := existingTransport.Current()
		if p.maxIdleConns != nil {
			t.MaxIdleConns = *p.maxIdleConns
		}
		if p.maxIdleConnsPerHost != nil {
			t.MaxIdleConnsPerHost = *p.maxIdleConnsPerHost
		}
		if p.disableHTTP2 != nil {
			t.DisableHTTP2 = *p.disableHTTP2
		}
		if p.idleConnTimeout != nil {
			t.IdleConnTimeout = *p.idleConnTimeout
		}
		if p.expectContinueTimeout != nil {
			t.ExpectContinueTimeout = *p.expectContinueTimeout
		}
		if p.responseHeaderTimeout != nil {
			t.ResponseHeaderTimeout = *p.responseHeaderTimeout
		}
		if p.tlsHandshakeTimeout != nil {
			t.TLSHandshakeTimeout = *p.tlsHandshakeTimeout
		}
		if p.proxyFromEnvironment != nil {
			t.ProxyFromEnvironment = *p.proxyFromEnvironment
		}
		if p.httpProxyURL != nil {
			t.HTTPProxyURL = p.httpProxyURL
		}
		if p.http2ReadIdleTimeout != nil {
			t.HTTP2ReadIdleTimeout = *p.http2ReadIdleTimeout
		}
		if p.http2PingTimeout != nil {
			t.HTTP2PingTimeout = *p.http2PingTimeout
		}
		return t
	})

	// TLS: overlay individual config fields onto existing TLS file params.
	existingTLS := b.tlsFileParams
	b.tlsFileParams = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) tlsFileParams {
		t := existingTLS.Current()
		if len(p.caFiles) > 0 {
			t.CAFiles = p.caFiles
		}
		// Only set cert/key when both are present — a lone cert or key file
		// would cause BuildTLSConfig to try to watch a file that may not exist.
		if p.certFile != "" && p.keyFile != "" {
			t.CertFile = p.certFile
			t.KeyFile = p.keyFile
		}
		if p.insecureSkipVerify != nil {
			t.InsecureSkipVerify = *p.insecureSkipVerify
		}
		if p.dynamicCertReload != nil {
			t.DynamicCertReload = *p.dynamicCertReload
		}
		return t
	})

	existingMaxAttempts := b.maxAttempts
	b.maxAttempts = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *int {
		if p.maxAttempts != nil {
			return p.maxAttempts
		}
		if existingMaxAttempts != nil {
			return existingMaxAttempts.Current()
		}
		return nil
	})

	existingInitialBackoff := b.initialBackoff
	b.initialBackoff = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.initialBackoff != nil {
			return *p.initialBackoff
		}
		return existingInitialBackoff.Current()
	})

	existingMaxBackoff := b.maxBackoff
	b.maxBackoff = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) time.Duration {
		if p.maxBackoff != nil {
			return *p.maxBackoff
		}
		return existingMaxBackoff.Current()
	})

	existingDisableMetrics := b.disableMetrics
	b.disableMetrics = refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) bool {
		if p.disableMetrics != nil {
			return *p.disableMetrics
		}
		return existingDisableMetrics.Current()
	})

	// Auth: install a single combined header provider for the refreshable case,
	// since the config may switch between token and basic auth on refresh. When
	// both refreshable values are nil, fall back to any provider installed by a
	// prior SetAuth*/SetBasicAuth* call so that builder-level auth survives
	// configs that omit credentials.
	existingAuth := b.authHeader
	apiToken := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *string { return p.apiToken })
	basicAuthR := refreshable.MapFromValidatedAuto(validParams, func(p validatedClientParams) *BasicAuth { return p.basicAuth })
	b.authHeader = func(ctx context.Context) (string, error) {
		if s := apiToken.Current(); s != nil {
			return bearerAuthHeader(*s), nil
		}
		if auth := basicAuthR.Current(); auth != nil {
			return basicAuthHeader(auth.User, auth.Password), nil
		}
		if existingAuth != nil {
			return existingAuth(ctx)
		}
		return "", nil
	}

	// Metrics tags via closure over validated refreshable.
	b.metricsTagProviders = append(b.metricsTagProviders, TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags {
		return validParams.Unvalidated().metricsTags
	}))

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

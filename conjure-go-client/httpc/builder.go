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
	"crypto/tls"
	"net/http"
	"slices"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
)

// BuilderAPI is the top-level builder interface. It composes
// [DialerBuilder], [TLSConfigBuilder], [TransportBuilder], and [ServiceBuilder]
// so a value satisfying BuilderAPI can be used wherever any of the four
// narrower interfaces is required.
//
// [Builder] is the concrete implementation; application code usually uses
// *Builder directly. The interfaces exist for generic helpers and mock
// generation:
//
//	func ApplyDefaults[B BuilderAPI[B]](b B) B {
//	    return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	}
type BuilderAPI[Self BuilderAPI[Self]] interface {
	DialerBuilder[Self]
	TLSConfigBuilder[Self]
	TransportBuilder[Self]
	ServiceBuilder[Self]
}

// BuilderCore holds every builder setting and implements all of [BuilderAPI]'s
// setters. It is generic over the embedding leaf type Self: each setter returns
// b.self, so a downstream builder that embeds *BuilderCore[*MyBuilder] inherits
// leaf-typed chaining (every Set* returns *MyBuilder) without re-declaring a
// single setter. [Builder] is the zero-extra-fields leaf.
//
// self is wired by [NewBuilderCore] and [BuilderCore.CloneCoreFor]; those (plus
// the leaf's own Clone) are the ONLY ways to construct a core, because self is
// unexported. A core whose self was never set returns a nil leaf from the first
// setter and panics on the next chained call, so never construct a bare
// &BuilderCore{} or &Builder{}.
//
// BuilderCore is NOT safe for concurrent use; Clone before mutating in another
// goroutine.
type BuilderCore[Self Cloneable[Self]] struct {
	self Self

	serviceName     refreshable.Refreshable[string]
	timeout         refreshable.Refreshable[time.Duration]
	dialerOverride  ContextDialer // escape hatch: replaces dialer construction
	dialerParams    refreshable.Refreshable[dialerParams]
	tlsConfig       *tls.Config // escape hatch: replaces all other TLS settings
	transportParams refreshable.Refreshable[transportParams]
	tlsFileParams   refreshable.Refreshable[tlsFileParams]
	tlsCABytes      refreshable.Refreshable[[][]byte]

	middlewares      []Middleware                // outer: applied after built-in middleware
	innerMiddlewares []Middleware                // inner: applied before built-in middleware
	auth             Authorizer                  // single auth slot; SetAuth (and its sugar) replace it
	headerValues     []requestValue[http.Header] // SetHeader/AddHeader contributors, resolved per request

	disableMetrics      refreshable.Refreshable[bool]
	metricsTagProviders []TagsProvider
	disableRequestSpan  bool
	disableRecovery     bool
	disableTraceHeaders bool
	disableTraceMetrics bool

	uris               refreshable.Refreshable[[]string]
	urlSelectorFactory func([]string) URLSelector
	allowEmptyURIs     bool

	maxAttempts    refreshable.Refreshable[*int]
	initialBackoff refreshable.Refreshable[time.Duration]
	maxBackoff     refreshable.Refreshable[time.Duration]

	transport        http.RoundTripper // escape hatch: direct transport injection
	caByteSlices     [][]byte
	clientCertKey    []byte
	clientCertCert   []byte
	includeSystemCAs bool
	errs             builderErrors
}

// Builder is the concrete [BuilderAPI] returned by [NewBuilder]: the
// zero-extra-fields leaf over [BuilderCore]. In addition to [BuilderAPI.Build],
// it exposes BuildDialer, BuildTLSConfig, BuildTransport, and BuildHTTPClient
// for building intermediate artifacts.
//
// Builder is NOT safe for concurrent use; call [Builder.Clone] before mutating
// in another goroutine.
type Builder struct {
	*BuilderCore[*Builder]
}

var _ BuilderAPI[*Builder] = (*Builder)(nil)

// NewBuilder creates a [Builder] seeded with sane defaults.
func NewBuilder() *Builder {
	b := &Builder{}
	b.BuilderCore = NewBuilderCore(b)
	return b
}

// NewBuilderCore creates a [BuilderCore] seeded with the same defaults as
// [NewBuilder] and wired to the given leaf. Downstream builders embedding
// *BuilderCore[*MyBuilder] call this from their own constructor:
//
//	func NewMyBuilder() *MyBuilder {
//	    b := &MyBuilder{}
//	    b.BuilderCore = httpc.NewBuilderCore(b)
//	    return b
//	}
//
// self must be the pointer that embeds the returned core; passing anything else
// breaks leaf-typed chaining (see [BuilderCore]).
func NewBuilderCore[Self Cloneable[Self]](self Self) *BuilderCore[Self] {
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
	return &BuilderCore[Self]{
		self:        self,
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
		initialBackoff:   refreshable.New(defaultInitialBackoff),
		maxBackoff:       refreshable.New(defaultMaxBackoff),
		includeSystemCAs: true,
	}
}

// Clone returns a deep copy of the builder. Refreshable fields are shared
// (the original and the clone observe the same source).
func (b *Builder) Clone() *Builder {
	c := &Builder{}
	c.BuilderCore = b.BuilderCore.CloneCoreFor(c)
	return c
}

// CloneCoreFor returns a deep copy of the core rebound to self, so the clone's
// setters return the new leaf rather than the original. Refreshable fields are
// shared (original and clone observe the same source); tlsConfig is deep-cloned
// and slices/maps are copied so mutating one builder's collections never affects
// the other. A leaf's Clone delegates here:
//
//	func (b *MyBuilder) Clone() *MyBuilder {
//	    c := &MyBuilder{ /* copy leaf fields */ }
//	    c.BuilderCore = b.BuilderCore.CloneCoreFor(c)
//	    return c
//	}
//
// There is deliberately no no-arg Clone on the core: a naive field copy would
// leave the clone's self pointing at the original, so each leaf must supply the
// fresh self.
func (b *BuilderCore[Self]) CloneCoreFor(self Self) *BuilderCore[Self] {
	var clonedTLSConfig *tls.Config
	if b.tlsConfig != nil {
		clonedTLSConfig = b.tlsConfig.Clone()
	}
	clone := &BuilderCore[Self]{
		self:                self,
		serviceName:         b.serviceName,
		timeout:             b.timeout,
		dialerOverride:      b.dialerOverride,
		dialerParams:        b.dialerParams,
		transportParams:     b.transportParams,
		tlsFileParams:       b.tlsFileParams,
		tlsConfig:           clonedTLSConfig,
		tlsCABytes:          b.tlsCABytes,
		middlewares:         slices.Clone(b.middlewares),
		innerMiddlewares:    slices.Clone(b.innerMiddlewares),
		auth:                b.auth,
		headerValues:        slices.Clone(b.headerValues),
		disableMetrics:      b.disableMetrics,
		metricsTagProviders: slices.Clone(b.metricsTagProviders),
		disableRequestSpan:  b.disableRequestSpan,
		disableRecovery:     b.disableRecovery,
		disableTraceHeaders: b.disableTraceHeaders,
		disableTraceMetrics: b.disableTraceMetrics,
		uris:                b.uris,
		urlSelectorFactory:  b.urlSelectorFactory,
		allowEmptyURIs:      b.allowEmptyURIs,
		maxAttempts:         b.maxAttempts,
		initialBackoff:      b.initialBackoff,
		maxBackoff:          b.maxBackoff,
		transport:           b.transport,
		caByteSlices:        slices.Clone(b.caByteSlices),
		clientCertKey:       bytes.Clone(b.clientCertKey),
		clientCertCert:      bytes.Clone(b.clientCertCert),
		includeSystemCAs:    b.includeSystemCAs,
		errs:                b.errs.clone(),
	}
	return clone
}

// Apply applies the given Param functions to the builder in sequence.
func (b *BuilderCore[Self]) Apply(params ...Param[Self]) Self {
	for _, p := range params {
		if p != nil {
			p(b.self)
		}
	}
	return b.self
}

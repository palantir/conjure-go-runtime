// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

// Package httpclient provides round trippers/transport wrappers for http clients.
package httpclient

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

const (
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
	defaultClientTimeout         = 1 * time.Minute
)

type clientBuilder struct {
	httpClientBuilder

	uris                   refreshable.StringSlice
	maxRetries             int
	enableUnlimitedRetries bool
	backoffOptions         []retry.Option

	errorDecoder                  ErrorDecoder
	disableTraceHeaderPropagation bool
}

type httpClientBuilder struct {
	ServiceName string

	// http.Client modifiers
	Timeout           refreshable.Duration
	Middlewares       []Middleware
	metricsMiddleware Middleware

	// http.Transport modifiers
	MaxIdleConns          refreshable.Int
	MaxIdleConnsPerHost   refreshable.Int
	Proxy                 func(*http.Request) (*url.URL, error)
	ProxyDialerBuilder    func(*net.Dialer) (proxy.Dialer, error)
	TLSClientConfig       *tls.Config
	DisableHTTP2          bool
	DisableRecovery       bool
	DisableTracing        bool
	DisableKeepAlives     bool
	IdleConnTimeout       refreshable.Duration
	TLSHandshakeTimeout   refreshable.Duration
	ExpectContinueTimeout refreshable.Duration
	ResponseHeaderTimeout time.Duration

	// http.Dialer modifiers
	DialTimeout time.Duration
	KeepAlive   time.Duration
	EnableIPV6  bool

	BytesBufferPool bytesbuffers.Pool
}

// NewClient returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClient(params ...ClientParam) (Client, error) {
	b := &clientBuilder{
		httpClientBuilder: *getDefaultHTTPClientBuilder(),
		backoffOptions:    []retry.Option{retry.WithInitialBackoff(250 * time.Millisecond)},
		errorDecoder:      restErrorDecoder{},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	client, middlewares, err := httpClientAndRoundTripHandlersFromBuilder(&b.httpClientBuilder)
	if err != nil {
		return nil, err
	}
	var edm Middleware
	if b.errorDecoder != nil {
		edm = errorDecoderMiddleware(b.errorDecoder)
	}

	if b.enableUnlimitedRetries {
		// max retries of 0 indicates no limit
		b.maxRetries = 0
	} else if b.maxRetries == 0 {
		if b.uris != nil {
			b.maxRetries = 2 * len(b.uris.CurrentStringSlice())
		}
	}

	c := &clientImpl{
		client:                        *client,
		uris:                          b.uris,
		maxRetries:                    b.maxRetries,
		backoffOptions:                b.backoffOptions,
		disableTraceHeaderPropagation: b.disableTraceHeaderPropagation,
		middlewares:                   middlewares,
		metricsMiddleware:             b.metricsMiddleware,
		errorDecoderMiddleware:        edm,
		bufferPool:                    b.BytesBufferPool,
	}

	// watch for config updates that require the transport to be updated
	b.handleIdleConnUpdate(c)
	b.handleTLSHandshakeTimeoutUpdate(c)
	b.handleExpectContinueTimeoutUpdate(c)
	b.handleMaxIdleConnsUpdate(c)
	b.handleMaxIdleConnsPerHostUpdate(c)
	b.handleHTTPClientTimeoutUpdate(c)

	return c, nil
}

func getDefaultHTTPClientBuilder() *httpClientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &httpClientBuilder{
		// These values are primarily pulled from http.DefaultTransport.
		TLSClientConfig:       defaultTLSConfig,
		Timeout:               refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultClientTimeout)),
		DialTimeout:           30 * time.Second,
		KeepAlive:             30 * time.Second,
		EnableIPV6:            false,
		DisableHTTP2:          false,
		IdleConnTimeout:       refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultIdleConnTimeout)),
		TLSHandshakeTimeout:   refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultTLSHandshakeTimeout)),
		ExpectContinueTimeout: refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultExpectContinueTimeout)),
		// These are higher than the defaults, but match Java and
		// heuristically work better for our relatively large services.
		MaxIdleConns:        refreshable.NewInt(refreshable.NewDefaultRefreshable(defaultMaxIdleConns)),
		MaxIdleConnsPerHost: refreshable.NewInt(refreshable.NewDefaultRefreshable(defaultMaxIdleConnsPerHost)),
	}
}

// NewHTTPClient returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	b := getDefaultHTTPClientBuilder()
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	client, roundTrippers, err := httpClientAndRoundTripHandlersFromBuilder(b)
	if err != nil {
		return nil, err
	}
	if b.metricsMiddleware != nil {
		client.Transport = wrapTransport(client.Transport, b.metricsMiddleware)
	}
	for _, handler := range roundTrippers {
		client.Transport = wrapTransport(client.Transport, handler)
	}
	return client, err
}

func httpClientAndRoundTripHandlersFromBuilder(b *httpClientBuilder) (*http.Client, []Middleware, error) {
	dialer, err := newDialer(b)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          b.MaxIdleConns.CurrentInt(),
		MaxIdleConnsPerHost:   b.MaxIdleConnsPerHost.CurrentInt(),
		TLSClientConfig:       b.TLSClientConfig,
		DisableKeepAlives:     b.DisableKeepAlives,
		ExpectContinueTimeout: b.ExpectContinueTimeout.CurrentDuration(),
		IdleConnTimeout:       b.IdleConnTimeout.CurrentDuration(),
		TLSHandshakeTimeout:   b.TLSHandshakeTimeout.CurrentDuration(),
		ResponseHeaderTimeout: b.ResponseHeaderTimeout,
	}
	if b.Proxy != nil && b.ProxyDialerBuilder == nil {
		transport.Proxy = b.Proxy
	}
	if !b.DisableHTTP2 {
		if err := http2.ConfigureTransport(transport); err != nil {
			return nil, nil, werror.Wrap(err, "failed to configure transport for http2")
		}
	}
	if !b.DisableTracing {
		_ = WithMiddleware(&traceMiddleware{ServiceName: b.ServiceName}).applyHTTPClient(b)
	}
	if !b.DisableRecovery {
		_ = WithMiddleware(&recoveryMiddleware{}).applyHTTPClient(b)
	}

	return &http.Client{
		Timeout:   b.Timeout.CurrentDuration(),
		Transport: transport,
	}, b.Middlewares, nil
}

// contextDialer is the newer interface implemented by net.Dialer and proxy.Dialer
type contextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

func newDialer(b *httpClientBuilder) (contextDialer, error) {
	netDialer := &net.Dialer{
		Timeout:   b.DialTimeout,
		KeepAlive: b.KeepAlive,
		DualStack: b.EnableIPV6,
	}

	resultDialer := contextDialer(netDialer)

	if b.ProxyDialerBuilder != nil {
		// Used for socks5 proxying
		proxyDialer, err := b.ProxyDialerBuilder(netDialer)
		if err != nil {
			return nil, err
		}
		if cDialer, ok := proxyDialer.(contextDialer); ok {
			resultDialer = cDialer
		} else {
			resultDialer = noopContextDialer{Dial: proxyDialer.Dial}
		}
	}

	if b.metricsMiddleware != nil {
		serviceNameTag, err := metrics.NewTag(MetricTagServiceName, b.ServiceName)
		if err != nil {
			return nil, err // should never happen, already checked by MetricsMiddleware()
		}
		resultDialer = &metricsWrappedDialer{dialer: resultDialer, serviceNameTag: serviceNameTag}
	}

	return resultDialer, nil
}

// noopContextDialer handles old proxy dialers that do not support context.
type noopContextDialer struct {
	Dial func(network, addr string) (net.Conn, error)
}

func (n noopContextDialer) DialContext(_ context.Context, network, addr string) (net.Conn, error) {
	return n.Dial(network, addr)
}

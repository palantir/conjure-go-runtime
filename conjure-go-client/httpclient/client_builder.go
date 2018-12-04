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
	"crypto/tls"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/pkg/tlsconfig"
	"github.com/palantir/witchcraft-go-error"
	"golang.org/x/net/http2"
)

type clientBuilder struct {
	httpClientBuilder

	uris           []string
	maxRetries     int
	backoffOptions []retry.Option

	disableRestErrors             bool
	disableTraceHeaderPropagation bool
}

type httpClientBuilder struct {
	ServiceName string

	// http.Client modifiers
	Timeout     time.Duration
	Middlewares []Middleware

	// http.Transport modifiers
	MaxIdleConns        int
	MaxIdleConnsPerHost int
	Proxy               func(*http.Request) (*url.URL, error)
	TLSClientConfig     *tls.Config
	DisableHTTP2        bool
	DisableRecovery     bool
	DisableTracing      bool

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
	if !b.disableRestErrors {
		middlewares = append(middlewares, &errorMiddleware{})
	}

	if b.maxRetries == 0 {
		b.maxRetries = 2 * len(b.uris)
	}

	for _, middleware := range middlewares {
		client.Transport = wrapTransport(client.Transport, middleware)
	}
	return &clientImpl{
		client:                        *client,
		uris:                          b.uris,
		maxRetries:                    b.maxRetries,
		backoffOptions:                b.backoffOptions,
		disableTraceHeaderPropagation: b.disableTraceHeaderPropagation,
	}, nil
}

func getDefaultHTTPClientBuilder() *httpClientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &httpClientBuilder{
		TLSClientConfig:     defaultTLSConfig,
		Timeout:             1 * time.Minute,
		DialTimeout:         30 * time.Second,
		KeepAlive:           30 * time.Second,
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 32,
		EnableIPV6:          false,
		DisableHTTP2:        false,
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
	for _, handler := range roundTrippers {
		client.Transport = wrapTransport(client.Transport, handler)
	}
	return client, err
}

func httpClientAndRoundTripHandlersFromBuilder(b *httpClientBuilder) (*http.Client, []Middleware, error) {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   b.DialTimeout,
			KeepAlive: b.KeepAlive,
			DualStack: b.EnableIPV6,
		}).DialContext,
		MaxIdleConns:          b.MaxIdleConns,
		MaxIdleConnsPerHost:   b.MaxIdleConnsPerHost,
		Proxy:                 b.Proxy,
		TLSClientConfig:       b.TLSClientConfig,
		ExpectContinueTimeout: http.DefaultTransport.(*http.Transport).ExpectContinueTimeout,
		IdleConnTimeout:       http.DefaultTransport.(*http.Transport).IdleConnTimeout,
		TLSHandshakeTimeout:   http.DefaultTransport.(*http.Transport).TLSHandshakeTimeout,
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
		Timeout:   b.Timeout,
		Transport: transport,
	}, b.Middlewares, nil
}

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
	"fmt"
	"net/http"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	refreshablev1 "github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

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

var (
	// ErrEmptyURIs is returned when the client expects to have base URIs configured to make requests, but the URIs are empty.
	// This check occurs in two places: when the client is constructed and when a request is executed.
	// To avoid the construction validation, use WithAllowCreateWithEmptyURIs().
	ErrEmptyURIs = fmt.Errorf("httpclient URLs must not be empty")
)

type clientBuilder struct {
	HTTP *httpClientBuilder

	URIs             refreshable.Refreshable[[]string]
	URIScorerBuilder func([]string) internal.URIScoringMiddleware

	// If false, NewClient() will return an error when URIs.Current() is empty.
	// This allows for a refreshable URI slice to be populated after construction but before use.
	AllowEmptyURIs bool

	ErrorDecoder ErrorDecoder

	BytesBufferPool bytesbuffers.Pool
	MaxAttempts     refreshable.Refreshable[*int]
	RetryParams     refreshable.Refreshable[refreshingclient.RetryParams]
}

type httpClientBuilder struct {
	ServiceName     refreshable.Refreshable[string]
	Timeout         refreshable.Refreshable[time.Duration]
	DialerParams    refreshable.Refreshable[refreshingclient.DialerParams]
	TLSConfig       *tls.Config // If unset, config in TransportParams will be used.
	TransportParams refreshable.Refreshable[refreshingclient.TransportParams]
	Middlewares     []Middleware

	DisableMetrics      refreshable.Refreshable[bool]
	MetricsTagProviders []TagsProvider

	// These middleware options are not refreshed anywhere because they are not in ClientConfig,
	// but they could be made refreshable if ever needed.
	DisableRequestSpan  bool
	DisableRecovery     bool
	DisableTraceHeaders bool
}

func (b *httpClientBuilder) Build(ctx context.Context, params ...HTTPClientParam) (RefreshableHTTPClient, error) {
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}

	var tlsProvider refreshingclient.TLSProvider
	if b.TLSConfig != nil {
		tlsProvider = refreshingclient.NewStaticTLSConfigProvider(b.TLSConfig)
	} else {
		tlsParams := refreshable.View(b.TransportParams, refreshingclient.TransportParams.GetTLSParams)
		refreshableProvider, err := refreshingclient.NewRefreshableTLSConfig(ctx, tlsParams)
		if err != nil {
			return nil, err
		}
		tlsProvider = refreshableProvider
	}

	dialer := refreshingclient.NewRefreshableDialer(ctx, b.DialerParams)
	transport := refreshingclient.NewRefreshableTransport(ctx, b.TransportParams, tlsProvider, dialer)
	transport = wrapTransport(transport, newMetricsMiddleware(b.ServiceName, b.MetricsTagProviders, b.DisableMetrics))
	transport = wrapTransport(transport, newTraceMiddleware(b.ServiceName, b.DisableRequestSpan, b.DisableTraceHeaders))
	if !b.DisableRecovery {
		transport = wrapTransport(transport, recoveryMiddleware{})
	}
	transport = wrapTransport(transport, b.Middlewares...)
	client, _ := refreshable.Map(b.Timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Transport: transport,
			Timeout:   timeout,
		}
	})
	return client, nil
}

// NewClient returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClient(params ...ClientParam) (Client, error) {
	b := newClientBuilder()
	return newClient(context.TODO(), b, params...)
}

// NewClientFromRefreshableConfig returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClientFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, params ...ClientParam) (Client, error) {
	return NewClientFromRefreshableV2Config(ctx, refreshablev1.ToV2[ClientConfig](config), params...)
}

// NewClientFromRefreshableV2Config returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClientFromRefreshableV2Config(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...ClientParam) (Client, error) {
	b := newClientBuilder()
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil); err != nil {
		return nil, err
	}
	return newClient(ctx, b, params...)
}

func newClient(ctx context.Context, b *clientBuilder, params ...ClientParam) (Client, error) {
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	if b.URIs == nil {
		return nil, werror.ErrorWithContextParams(ctx, "httpclient URLs must be set in configuration or by constructor param", werror.SafeParam("serviceName", b.HTTP.ServiceName.Current()))
	}
	if !b.AllowEmptyURIs && len(b.URIs.Current()) == 0 {
		return nil, werror.WrapWithContextParams(ctx, ErrEmptyURIs, "", werror.SafeParam("serviceName", b.HTTP.ServiceName.Current()))
	}

	var edm Middleware
	if b.ErrorDecoder != nil {
		edm = errorDecoderMiddleware{errorDecoder: b.ErrorDecoder}
	}

	middleware := b.HTTP.Middlewares
	b.HTTP.Middlewares = nil

	httpClient, err := b.HTTP.Build(ctx)
	if err != nil {
		return nil, err
	}

	var recovery Middleware
	if !b.HTTP.DisableRecovery {
		recovery = recoveryMiddleware{}
	}
	uriScorer := refreshable.View(b.URIs, func(uris []string) internal.URIScoringMiddleware {
		if b.URIScorerBuilder == nil {
			return internal.NewBalancedURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
		return b.URIScorerBuilder(uris)
	})
	return &clientImpl{
		serviceName:            b.HTTP.ServiceName,
		client:                 httpClient,
		uriScorer:              uriScorer,
		maxAttempts:            b.MaxAttempts,
		backoffOptions:         b.RetryParams,
		middlewares:            middleware,
		errorDecoderMiddleware: edm,
		recoveryMiddleware:     recovery,
		bufferPool:             b.BytesBufferPool,
	}, nil
}

// NewHTTPClient returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	b := newClientBuilder()
	provider, err := b.HTTP.Build(context.TODO(), params...)
	if err != nil {
		return nil, err
	}
	return provider.Current(), nil
}

// RefreshableHTTPClient exposes the internal interface
type RefreshableHTTPClient refreshable.Refreshable[*http.Client]

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...HTTPClientParam) (refreshable.Refreshable[*http.Client], error) {
	b := newClientBuilder()
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil); err != nil {
		return nil, err
	}
	return b.HTTP.Build(ctx, params...)
}

func newClientBuilder() *clientBuilder {
	return &clientBuilder{
		HTTP: &httpClientBuilder{
			ServiceName: refreshable.New(""),
			Timeout:     refreshable.New(defaultHTTPTimeout),
			DialerParams: refreshable.New(refreshingclient.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			}),
			TransportParams: refreshable.New(refreshingclient.TransportParams{
				MaxIdleConns:          defaultMaxIdleConns,
				MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
				DisableHTTP2:          false,
				DisableKeepAlives:     false,
				IdleConnTimeout:       defaultIdleConnTimeout,
				ExpectContinueTimeout: defaultExpectContinueTimeout,
				ResponseHeaderTimeout: 0,
				TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
				HTTPProxyURL:          nil,
				ProxyFromEnvironment:  true,
				HTTP2ReadIdleTimeout:  defaultHTTP2ReadIdleTimeout,
				HTTP2PingTimeout:      defaultHTTP2PingTimeout,
			}),
			Middlewares:         nil,
			DisableMetrics:      refreshable.New(false),
			MetricsTagProviders: nil,
			DisableRecovery:     false,
			DisableRequestSpan:  false,
			DisableTraceHeaders: false,
		},
		URIs:            nil,
		BytesBufferPool: nil,
		ErrorDecoder:    restErrorDecoder{},
		MaxAttempts:     nil,
		RetryParams: refreshable.New(refreshingclient.RetryParams{
			InitialBackoff: defaultInitialBackoff,
			MaxBackoff:     defaultMaxBackoff,
		}),
	}
}

func newClientBuilderFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], b *clientBuilder, reloadErrorSubmitter func(error)) error {
	security := refreshable.View(config, func(c ClientConfig) SecurityConfig {
		return c.Security
	})
	if tlsConfig, err := subscribeTLSConfigUpdateWarning(ctx, security); err != nil {
		return err
	} else if tlsConfig != nil {
		b.HTTP.TLSConfig = tlsConfig
	}

	validParams, _, err := refreshable.MapWithError(config, func(c ClientConfig) (refreshingclient.ValidatedClientParams, error) {
		p, err := newValidatedClientParamsFromConfig(ctx, c)
		if reloadErrorSubmitter != nil {
			reloadErrorSubmitter(err)
		}
		return p, err
	})
	if err != nil {
		return err
	}

	b.HTTP.ServiceName = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetServiceName)
	b.HTTP.DialerParams = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetDialerParams)
	b.HTTP.TransportParams = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetTransport)
	b.HTTP.Timeout = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetTimeout)
	b.HTTP.DisableMetrics = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetDisableMetrics)
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders,
		TagsProviderFunc(func(*http.Request, *http.Response, error) metrics.Tags {
			return validParams.Current().MetricsTags
		}))
	b.HTTP.Middlewares = append(b.HTTP.Middlewares,
		newAuthTokenMiddlewareFromRefreshable(refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetAPIToken)),
		newBasicAuthMiddlewareFromRefreshable(refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetBasicAuth)))

	b.URIs = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetURIs)
	b.MaxAttempts = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetMaxAttempts)
	b.RetryParams = refreshable.View(validParams, refreshingclient.ValidatedClientParams.GetRetry)
	return nil
}

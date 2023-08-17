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
	"net/http"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
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

type clientBuilder struct {
	HTTP *httpClientBuilder

	URIs             refreshable.Refreshable[[]string]
	URIScorerBuilder func([]string) internal.URIScoringMiddleware

	ErrorDecoder ErrorDecoder

	BytesBufferPool bytesbuffers.Pool
	MaxAttempts     refreshable.Refreshable[*int]
	RetryParams     refreshable.Refreshable[refreshingclient.RetryParams]
}

type httpClientBuilder struct {
	ServiceNameTag  metrics.Tag // Service name is not refreshable.
	Timeout         refreshable.Refreshable[time.Duration]
	DialerParams    refreshable.Refreshable[refreshingclient.DialerParams]
	TLSConfig       *tls.Config // TODO: Make this refreshing and wire into transport
	TransportParams refreshable.Refreshable[refreshingclient.TransportParams]
	Middlewares     []Middleware

	DisableMetrics      refreshable.Refreshable[bool]
	MetricsTagProviders []TagsProvider

	// These middleware options are not refreshed anywhere because they are not in ClientConfig,
	// but they could be made refreshable if ever needed.
	CreateRequestSpan  bool
	DisableRecovery    bool
	InjectTraceHeaders bool
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
	transport := refreshingclient.NewRefreshableTransport(ctx,
		b.TransportParams,
		b.TLSConfig,
		refreshingclient.NewRefreshableDialer(ctx, b.DialerParams))
	transport = wrapTransport(transport, newMetricsMiddleware(b.ServiceNameTag, b.MetricsTagProviders, b.DisableMetrics))
	transport = wrapTransport(transport, traceMiddleware{
		ServiceName:       b.ServiceNameTag.Value(),
		CreateRequestSpan: b.CreateRequestSpan,
		InjectHeaders:     b.InjectTraceHeaders,
	})
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
func NewClientFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...ClientParam) (Client, error) {
	b := newClientBuilder()
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil, false); err != nil {
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
		return nil, werror.Error("httpclient URLs must not be empty", werror.SafeParam("serviceName", b.HTTP.ServiceNameTag.Value()))
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
	uriScorer, _ := refreshable.Map(b.URIs, func(uris []string) internal.URIScoringMiddleware {
		if b.URIScorerBuilder == nil {
			return internal.NewBalancedURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
		return b.URIScorerBuilder(uris)
	})
	return &clientImpl{
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
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil, true); err != nil {
		return nil, err
	}
	return b.HTTP.Build(ctx, params...)
}

func newClientBuilder() *clientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &clientBuilder{
		HTTP: &httpClientBuilder{
			ServiceNameTag: metrics.Tag{},
			Timeout:        refreshable.New(defaultHTTPTimeout),
			DialerParams: refreshable.New(refreshingclient.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			}),
			TLSConfig: defaultTLSConfig,
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
			DisableMetrics:      refreshable.New(false),
			DisableRecovery:     false,
			CreateRequestSpan:   true,
			InjectTraceHeaders:  true,
			MetricsTagProviders: nil,
			Middlewares:         nil,
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

func newClientBuilderFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], b *clientBuilder, reloadErrorSubmitter func(error), isHTTPClient bool) error {
	var err error
	b.HTTP.ServiceNameTag, err = metrics.NewTag(MetricTagServiceName, config.Current().ServiceName)
	if err != nil {
		return werror.WrapWithContextParams(ctx, err, "invalid service name metrics tag")
	}
	config.Subscribe(func(c ClientConfig) {
		if c.ServiceName != b.HTTP.ServiceNameTag.Value() {
			svc1log.FromContext(ctx).Warn("conjure-go-runtime: Service name changed but can not be live-reloaded.",
				svc1log.SafeParam("existingServiceName", b.HTTP.ServiceNameTag.Value()),
				svc1log.SafeParam("updatedServiceName", c.ServiceName))
		}
	})
	security, _ := refreshable.Map(config, func(c ClientConfig) SecurityConfig {
		return c.Security
	})
	if tlsConfig, err := subscribeTLSConfigUpdateWarning(ctx, security); err != nil {
		return err
	} else if tlsConfig != nil {
		b.HTTP.TLSConfig = tlsConfig
	}

	validParams, unsub, err := refreshable.MapWithError(config, func(c ClientConfig) (refreshingclient.ValidatedClientParams, error) {
		p, err := newValidatedClientParamsFromConfig(ctx, c, isHTTPClient)
		if reloadErrorSubmitter != nil {
			reloadErrorSubmitter(err)
		}
		return p, err
	})
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		unsub()
	}()

	b.HTTP.DialerParams = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetDialerParams)
	b.HTTP.TransportParams = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetTransport)
	b.HTTP.Timeout = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetTimeout)
	b.HTTP.DisableMetrics = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetDisableMetrics)
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders, refreshableMetricsTagsProvider{
		Refreshable: refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetMetricsTags),
	})
	b.HTTP.Middlewares = append(b.HTTP.Middlewares,
		newAuthTokenMiddlewareFromRefreshable(refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetAPIToken)),
		newBasicAuthMiddlewareFromRefreshable(refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetBasicAuth)),
	)
	b.URIs = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetURIs)
	b.MaxAttempts = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetMaxAttempts)
	b.RetryParams = refreshingclient.MapValidClientParams(validParams, refreshingclient.ValidatedClientParams.GetRetry)
	return nil
}

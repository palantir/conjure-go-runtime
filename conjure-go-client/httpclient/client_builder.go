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
	"github.com/palantir/pkg/refreshable"
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
	http *httpClientBuilder

	uris             refreshable.StringSlice
	uriScorerBuilder func([]string) internal.URIScoringMiddleware

	errorDecoder ErrorDecoder

	bytesBufferPool bytesbuffers.Pool
	maxAttempts     refreshable.IntPtr
	retryParams     refreshingclient.RefreshableRetryParams
}

type httpClientBuilder struct {
	serviceNameTag  metrics.Tag // Service name is not refreshable.
	timeout         refreshable.Duration
	dialerParams    refreshingclient.RefreshableDialerParams
	tlsConfig       *tls.Config // TODO: Make this refreshing and wire into transport
	transportParams refreshingclient.RefreshableTransportParams
	middlewares     []Middleware

	disableMetrics      refreshable.Bool
	metricsTagProviders []TagsProvider

	// These middleware options are not refreshed anywhere because they are not in ClientConfig,
	// but they could be made refreshable if ever needed.
	createRequestSpan  bool
	disableRecovery    bool
	injectTraceHeaders bool
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
		b.transportParams,
		b.tlsConfig,
		refreshingclient.NewRefreshableDialer(ctx, b.dialerParams))
	transport = wrapTransport(transport, newMetricsMiddleware(b.serviceNameTag, b.metricsTagProviders, b.disableMetrics))
	transport = wrapTransport(transport, traceMiddleware{
		serviceName:       b.serviceNameTag.Value(),
		createRequestSpan: b.createRequestSpan,
		injectHeaders:     b.injectTraceHeaders,
	})
	if !b.disableRecovery {
		transport = wrapTransport(transport, recoveryMiddleware{})
	}
	transport = wrapTransport(transport, b.middlewares...)

	return refreshingclient.NewRefreshableHTTPClient(transport, b.timeout), nil
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
	if b.uris == nil {
		return nil, werror.Error("httpclient URLs must not be empty", werror.SafeParam("serviceName", b.http.serviceNameTag.Value()))
	}

	var edm Middleware
	if b.errorDecoder != nil {
		edm = errorDecoderMiddleware{errorDecoder: b.errorDecoder}
	}

	middleware := b.http.middlewares
	b.http.middlewares = nil

	httpClient, err := b.http.Build(ctx)
	if err != nil {
		return nil, err
	}

	var recovery Middleware
	if !b.http.disableRecovery {
		recovery = recoveryMiddleware{}
	}
	uriScorer := internal.NewRefreshableURIScoringMiddleware(b.uris, func(uris []string) internal.URIScoringMiddleware {
		if b.uriScorerBuilder == nil {
			return internal.NewBalancedURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
		return b.uriScorerBuilder(uris)
	})
	return &clientImpl{
		client:                 httpClient,
		uriScorer:              uriScorer,
		maxAttempts:            b.maxAttempts,
		backoffOptions:         b.retryParams,
		middlewares:            middleware,
		errorDecoderMiddleware: edm,
		recoveryMiddleware:     recovery,
		bufferPool:             b.bytesBufferPool,
	}, nil
}

// NewHTTPClient returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	b := newClientBuilder()
	provider, err := b.http.Build(context.TODO(), params...)
	if err != nil {
		return nil, err
	}
	return provider.CurrentHTTPClient(), nil
}

// RefreshableHTTPClient exposes the internal interface
type RefreshableHTTPClient = refreshingclient.RefreshableHTTPClient

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, params ...HTTPClientParam) (RefreshableHTTPClient, error) {
	b := newClientBuilder()
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil, true); err != nil {
		return nil, err
	}
	return b.http.Build(ctx, params...)
}

func newClientBuilder() *clientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &clientBuilder{
		http: &httpClientBuilder{
			serviceNameTag: metrics.Tag{},
			timeout:        refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultHTTPTimeout)),
			dialerParams: refreshingclient.NewRefreshingDialerParams(refreshable.NewDefaultRefreshable(refreshingclient.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			})),
			tlsConfig: defaultTLSConfig,
			transportParams: refreshingclient.NewRefreshingTransportParams(refreshable.NewDefaultRefreshable(refreshingclient.TransportParams{
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
			})),
			disableMetrics:      refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			disableRecovery:     false,
			createRequestSpan:   true,
			injectTraceHeaders:  true,
			metricsTagProviders: nil,
			middlewares:         nil,
		},
		uris:            nil,
		bytesBufferPool: nil,
		errorDecoder:    restErrorDecoder{},
		maxAttempts:     nil,
		retryParams: refreshingclient.NewRefreshingRetryParams(refreshable.NewDefaultRefreshable(refreshingclient.RetryParams{
			InitialBackoff: defaultInitialBackoff,
			MaxBackoff:     defaultMaxBackoff,
		})),
	}
}

func newClientBuilderFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, b *clientBuilder, reloadErrorSubmitter func(error), isHTTPClient bool) error {
	var err error
	b.http.serviceNameTag, err = metrics.NewTag(MetricTagServiceName, config.CurrentClientConfig().ServiceName)
	if err != nil {
		return werror.WrapWithContextParams(ctx, err, "invalid service name metrics tag")
	}
	config.ServiceName().SubscribeToString(func(s string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: Service name changed but can not be live-reloaded.",
			svc1log.SafeParam("existingServiceName", b.http.serviceNameTag.Value()),
			svc1log.SafeParam("updatedServiceName", s))
	})

	if tlsConfig, err := subscribeTLSConfigUpdateWarning(ctx, config.Security()); err != nil {
		return err
	} else if tlsConfig != nil {
		b.http.tlsConfig = tlsConfig
	}

	refreshingParams, err := refreshable.NewMapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		p, err := newValidatedClientParamsFromConfig(ctx, i.(ClientConfig), isHTTPClient)
		if reloadErrorSubmitter != nil {
			reloadErrorSubmitter(err)
		}
		return p, err
	})
	validParams := refreshingclient.NewRefreshingValidatedClientParams(refreshingParams)
	if err != nil {
		return err
	}

	b.http.dialerParams = validParams.Dialer()
	b.http.transportParams = validParams.Transport()
	b.http.timeout = validParams.Timeout()
	b.http.disableMetrics = validParams.DisableMetrics()
	b.http.metricsTagProviders = append(b.http.metricsTagProviders, refreshableMetricsTagsProvider{validParams.MetricsTags()})
	b.http.middlewares = append(b.http.middlewares,
		newAuthTokenMiddlewareFromRefreshable(validParams.APIToken()),
		newBasicAuthMiddlewareFromRefreshable(validParams.BasicAuth()))

	b.uris = validParams.URIs()
	b.maxAttempts = validParams.MaxAttempts()
	b.retryParams = validParams.Retry()
	return nil
}

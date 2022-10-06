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
	rclient "github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
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
	RetryParams     refreshable.Refreshable[rclient.RetryParams]
}

type httpClientBuilder struct {
	Config refreshable.Refreshable[ClientConfig]

	ServiceNameTag  metrics.Tag // Service name is not refreshable.
	Timeout         refreshable.Refreshable[time.Duration]
	DialerParams    refreshable.Refreshable[rclient.DialerParams]
	TLSConfig       *tls.Config // TODO: Make this refreshing and wire into transport
	TransportParams refreshable.Refreshable[rclient.TransportParams]
	Middlewares     []Middleware

	DisableMetrics      refreshable.Refreshable[bool]
	MetricsTagProviders []TagsProvider

	// These middleware options are not refreshed anywhere because they are not in ClientConfig,
	// but they could be made refreshable if ever needed.
	CreateRequestSpan  bool
	DisableRecovery    bool
	InjectTraceHeaders bool
}

func (b *httpClientBuilder) Build(ctx context.Context, params ...HTTPClientParam) (refreshable.Refreshable[*http.Client], error) {
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	dialer := rclient.NewRefreshableDialer(ctx, b.DialerParams)
	transport := rclient.NewRefreshableTransport(ctx, b.TransportParams, b.TLSConfig, dialer)
	transport = wrapTransport(transport, newMetricsMiddleware(b.ServiceNameTag, b.MetricsTagProviders, b.DisableMetrics.Current))
	transport = wrapTransport(transport, newTraceMiddleware(b.ServiceNameTag.Value(), b.CreateRequestSpan, b.InjectTraceHeaders))
	transport = wrapTransport(transport, &conditionalMiddleware{Delegate: recoveryMiddleware{}, Disabled: refreshable.New(b.DisableRecovery).Current})
	transport = wrapTransport(transport, b.Middlewares...)

	client := refreshable.MapContext(ctx, b.Timeout, func(timeout time.Duration) *http.Client {
		return &http.Client{
			Timeout:   timeout,
			Transport: transport,
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
	b.HTTP.Config = config
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
	if b.HTTP.Config != nil {
		if err := newClientBuilderFromRefreshableConfig(ctx, b, nil, false); err != nil {
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
	uriScorer := refreshable.MapContext(ctx, b.URIs, func(uris []string) internal.URIScoringMiddleware {
		if b.URIScorerBuilder == nil {
			return internal.NewBalancedURIScoringMiddleware(uris, func() int64 { return time.Now().UnixNano() })
		}
		return b.URIScorerBuilder(uris)
	})
	return &clientImpl{
		client:                 httpClient,
		uriScorer:              uriScorer,
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

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config refreshable.Refreshable[ClientConfig], params ...HTTPClientParam) (refreshable.Refreshable[*http.Client], error) {
	b := newClientBuilder()
	b.HTTP.Config = config
	if err := newClientBuilderFromRefreshableConfig(ctx, b, nil, true); err != nil {
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
			DialerParams: refreshable.New(rclient.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			}),
			TLSConfig: defaultTLSConfig,
			TransportParams: refreshable.New(rclient.TransportParams{
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
			DisableMetrics:      nil,
			DisableRecovery:     false,
			CreateRequestSpan:   true,
			InjectTraceHeaders:  true,
			MetricsTagProviders: nil,
			Middlewares:         nil,
		},
		URIs:            nil,
		BytesBufferPool: nil,
		ErrorDecoder:    restErrorDecoder{},
		RetryParams: refreshable.New(rclient.RetryParams{
			MaxAttempts:    nil,
			InitialBackoff: defaultInitialBackoff,
			MaxBackoff:     defaultMaxBackoff,
		}),
	}
}

func newClientBuilderFromRefreshableConfig(
	ctx context.Context,
	b *clientBuilder,
	reloadErrorSubmitter func(error),
	isHTTPClient bool,
) error {
	serviceName := refreshable.MapContext(ctx, b.HTTP.Config, func(cfg ClientConfig) string { return cfg.ServiceName })
	var err error
	b.HTTP.ServiceNameTag, err = metrics.NewTag(MetricTagServiceName, serviceName.Current())
	if err != nil {
		return werror.WrapWithContextParams(ctx, err, "invalid service name metrics tag")
	}
	serviceName.Subscribe(func(s string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: Service name changed but can not be live-reloaded.",
			svc1log.SafeParam("existingServiceName", b.HTTP.ServiceNameTag.Value()),
			svc1log.SafeParam("updatedServiceName", s))
	})

	if tlsConfig, err := subscribeTLSConfigUpdateWarning(ctx, b.HTTP.Config); err != nil {
		return err
	} else if tlsConfig != nil {
		b.HTTP.TLSConfig = tlsConfig
	}

	validParams, stop, err := refreshable.MapWithError(b.HTTP.Config, func(cfg ClientConfig) (rclient.ValidatedClientParams, error) {
		p, err := newValidatedClientParamsFromConfig(ctx, cfg, isHTTPClient)
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
		stop()
	}()

	b.HTTP.DialerParams = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetDialerParams)
	b.HTTP.TransportParams = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetTransport)
	b.HTTP.Timeout = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetTimeout)
	b.HTTP.DisableMetrics = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetDisableMetrics)
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders, refreshableMetricsTagsProvider{
		Refreshable: rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetMetricsTags),
	})
	b.HTTP.Middlewares = append(b.HTTP.Middlewares,
		newAuthTokenMiddlewareFromRefreshable(
			rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetAPIToken),
		),
		newBasicAuthMiddlewareFromRefreshable(ctx, refreshable.MapContext[rclient.ValidatedClientParams, *BasicAuth](ctx, validParams, func(p rclient.ValidatedClientParams) *BasicAuth {
			return (*BasicAuth)(p.BasicAuth)
		})),
	)
	b.URIs = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetURIs)
	b.RetryParams = rclient.MapValidClientParams(ctx, validParams, rclient.ValidatedClientParams.GetRetry)
	return nil
}

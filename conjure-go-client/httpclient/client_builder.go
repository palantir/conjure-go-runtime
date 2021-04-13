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
	"net/http"
	"net/url"
	"sort"
	"time"

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
	defaultInitialBackoff        = 2 * time.Second
	defaultMaxBackoff            = 250 * time.Millisecond
	defaultMultiplier            = float64(2)
	defaultRandomization         = 0.15
)

type clientBuilder struct {
	HTTP *httpClientBuilder

	URIs refreshable.StringSlice

	ErrorDecoder ErrorDecoder

	BytesBufferPool bytesbuffers.Pool
	MaxAttempts     refreshable.IntPtr // 0 means no limit. if nil, use 2*len(uris)
	RetryParams     refreshingclient.RefreshableRetryParams
}

type httpClientBuilder struct {
	ServiceNameTag  metrics.Tag // service name is not refreshable.
	Timeout         refreshable.Duration
	DialerParams    refreshingclient.RefreshableDialerParams
	TransportParams refreshingclient.RefreshableTransportParams
	Middlewares     []Middleware

	DisableMetrics      refreshable.Bool
	DisableRecovery     refreshable.Bool
	MetricsTagProviders []TagsProvider

	// These fields are not in configuration so not actually refreshed anywhere,
	// but treat them as refreshable in case that ever changes.
	CreateRequestSpan  refreshable.Bool
	InjectTraceHeaders refreshable.Bool
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
	dialer := &metricsWrappedDialer{
		Disabled:       b.DisableMetrics,
		ServiceNameTag: b.ServiceNameTag,
		Dialer:         refreshingclient.NewRefreshableDialer(ctx, b.DialerParams),
	}
	transport := refreshingclient.NewRefreshableTransport(ctx, b.TransportParams, dialer)
	transport = wrapTransport(transport,
		newMetricsMiddleware(b.ServiceNameTag, b.MetricsTagProviders, b.DisableMetrics),
		traceMiddleware{CreateRequestSpan: b.CreateRequestSpan, InjectHeaders: b.InjectTraceHeaders, ServiceName: b.ServiceNameTag.Value()},
		recoveryMiddleware{Disabled: b.DisableRecovery})
	transport = wrapTransport(transport, b.Middlewares...)

	return refreshingclient.NewRefreshableHTTPClient(ctx, transport, b.Timeout), nil
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
	b, _, err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil)
	if err != nil {
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

	return &clientImpl{
		client:                 httpClient,
		uris:                   b.URIs,
		maxAttempts:            b.MaxAttempts,
		retryOptions:           b.RetryParams,
		middlewares:            middleware,
		errorDecoderMiddleware: edm,
		recoveryMiddleware:     &recoveryMiddleware{Disabled: b.HTTP.DisableRecovery},
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
	return provider.CurrentHTTPClient(), nil
}

// RefreshableHTTPClient exposes the internal interface
type RefreshableHTTPClient = refreshingclient.RefreshableHTTPClient

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, params ...HTTPClientParam) (RefreshableHTTPClient, error) {
	b := newClientBuilder()
	b, _, err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil)
	if err != nil {
		return nil, err
	}
	return b.HTTP.Build(ctx, params...)
}

func newClientBuilder() *clientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &clientBuilder{
		HTTP: &httpClientBuilder{
			ServiceNameTag: metrics.Tag{},
			Timeout:        refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultHTTPTimeout)),
			DialerParams: refreshingclient.RefreshableDialerParams{Refreshable: refreshable.NewDefaultRefreshable(refreshingclient.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			})},
			TransportParams: refreshingclient.RefreshableTransportParams{Refreshable: refreshable.NewDefaultRefreshable(refreshingclient.TransportParams{
				ServiceNameTag:        metrics.Tag{},
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
				TLSConfig:             defaultTLSConfig,
			})},
			CreateRequestSpan:   refreshable.NewBool(refreshable.NewDefaultRefreshable(true)),
			InjectTraceHeaders:  refreshable.NewBool(refreshable.NewDefaultRefreshable(true)),
			DisableMetrics:      refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			DisableRecovery:     refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			MetricsTagProviders: nil,
			Middlewares:         nil,
		},
		URIs:            nil,
		BytesBufferPool: nil,
		ErrorDecoder:    restErrorDecoder{},
		MaxAttempts:     refreshable.NewIntPtr(refreshable.NewDefaultRefreshable((*int)(nil))),
		RetryParams: refreshingclient.RefreshableRetryParams{
			Refreshable: refreshable.NewDefaultRefreshable(refreshingclient.RetryParams{
				InitialBackoff:      newDurationPtr(defaultInitialBackoff),
				MaxBackoff:          newDurationPtr(defaultMaxBackoff),
				Multiplier:          newFloatPtr(defaultMultiplier),
				RandomizationFactor: newFloatPtr(defaultRandomization),
			}),
		},
	}
}

// validParamsSnapshot represents a set of fields derived from a snapshot of ClientConfig.
// It is designed for use within a refreshable: fields are comparable with reflect.DeepEqual
// so unnecessary updates are not pushed to subscribers.
type validParamsSnapshot struct {
	dialer         refreshingclient.DialerParams
	disableMetrics bool
	maxAttempts    *int
	metricsTags    metrics.Tags
	retry          refreshingclient.RetryParams
	timeout        time.Duration
	transport      refreshingclient.TransportParams
	uris           []string
}

func newClientBuilderFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, b *clientBuilder, reloadErrorSubmitter func(error)) (_ *clientBuilder, reloadError func() error, err error) {
	serviceNameTag, err := metrics.NewTag(MetricTagServiceName, config.CurrentClientConfig().ServiceName)
	if err != nil {
		return nil, nil, werror.Wrap(err, "invalid service name metrics tag")
	}
	config.ServiceName().SubscribeToString(func(s string) {
		svc1log.FromContext(ctx).Warn("Service name changed but can not be live-reloaded.",
			svc1log.SafeParam("existingServiceName", serviceNameTag.Value()),
			svc1log.SafeParam("updatedServiceName", s))
	})

	refreshingParams, err := refreshable.NewMapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		p, err := newValidParamsSnapshot(ctx, i.(ClientConfig), serviceNameTag)
		if reloadErrorSubmitter != nil {
			reloadErrorSubmitter(err)
		}
		return p, err
	})
	if err != nil {
		return nil, nil, err
	}
	b.HTTP.DialerParams.Refreshable = mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.dialer
	})
	b.HTTP.TransportParams.Refreshable = mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.transport
	})
	b.HTTP.Timeout = refreshable.NewDuration(mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.timeout
	}))
	b.HTTP.DisableMetrics = refreshable.NewBool(mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.disableMetrics
	}))
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders, refreshableMetricsTagsProvider{Refreshable: mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.metricsTags
	})})
	b.HTTP.ServiceNameTag = serviceNameTag

	b.MaxAttempts = refreshable.NewIntPtr(mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.maxAttempts
	}))
	b.RetryParams.Refreshable = mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.retry
	})
	b.URIs = refreshable.NewStringSlice(mapValidParamsSnapshot(refreshingParams, func(params validParamsSnapshot) interface{} {
		return params.uris
	}))

	return b, refreshingParams.LastValidateErr, nil
}

func newValidParamsSnapshot(ctx context.Context, config ClientConfig, serviceNameTag metrics.Tag) (validParamsSnapshot, error) {
	dialer := refreshingclient.DialerParams{
		DialTimeout: derefDurationPtr(config.ConnectTimeout, defaultDialTimeout),
		KeepAlive:   defaultKeepAlive,
	}

	tlsConfig, err := newTLSConfig(config.Security)
	if err != nil {
		return validParamsSnapshot{}, err
	}

	transport := refreshingclient.TransportParams{
		ServiceNameTag:        serviceNameTag,
		MaxIdleConns:          derefIntPtr(config.MaxIdleConns, defaultMaxIdleConns),
		MaxIdleConnsPerHost:   derefIntPtr(config.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost),
		DisableHTTP2:          derefBoolPtr(config.DisableHTTP2, false),
		IdleConnTimeout:       derefDurationPtr(config.IdleConnTimeout, defaultIdleConnTimeout),
		ExpectContinueTimeout: derefDurationPtr(config.ExpectContinueTimeout, defaultExpectContinueTimeout),
		ProxyFromEnvironment:  derefBoolPtr(config.ProxyFromEnvironment, false),
		TLSConfig:             tlsConfig,
		TLSHandshakeTimeout:   derefDurationPtr(config.TLSHandshakeTimeout, defaultTLSHandshakeTimeout),
	}

	if config.ProxyURL != nil {
		proxyURL, err := url.ParseRequestURI(*config.ProxyURL)
		if err != nil {
			return validParamsSnapshot{}, werror.WrapWithContextParams(ctx, err, "invalid proxy url")
		}
		switch proxyURL.Scheme {
		case "http", "https":
			transport.HTTPProxyURL = proxyURL
		case "socks5", "socks5h":
			dialer.SocksProxyURL = proxyURL
		default:
			return validParamsSnapshot{}, werror.WrapWithContextParams(ctx, err, "invalid proxy url: only http(s) and socks5 are supported")
		}
	}

	disableMetrics := config.Metrics.Enabled != nil && !*config.Metrics.Enabled

	metricsTags, err := metrics.NewTags(config.Metrics.Tags)
	if err != nil {
		return validParamsSnapshot{}, err
	}

	multiplier := defaultMultiplier
	randomization := defaultRandomization
	var maxAttempts *int
	if config.MaxNumRetries != nil {
		a := *config.MaxNumRetries + 1
		maxAttempts = &a
	}
	retry := refreshingclient.RetryParams{
		InitialBackoff:      config.InitialBackoff,
		MaxBackoff:          config.MaxBackoff,
		Multiplier:          &multiplier,
		RandomizationFactor: &randomization,
	}

	timeout := defaultHTTPTimeout
	if config.ReadTimeout != nil || config.WriteTimeout != nil {
		rt := derefDurationPtr(config.ReadTimeout, 0)
		wt := derefDurationPtr(config.WriteTimeout, 0)
		// return max of read and write
		if rt > wt {
			timeout = rt
		} else {
			timeout = wt
		}
	}

	var uris []string
	for _, uriStr := range config.URIs {
		if uriStr == "" {
			continue
		}
		if _, err := url.ParseRequestURI(uriStr); err != nil {
			return validParamsSnapshot{}, werror.WrapWithContextParams(ctx, err, "invalid url")
		}
		uris = append(uris, uriStr)
	}
	sort.Strings(uris)

	return validParamsSnapshot{
		dialer:         dialer,
		disableMetrics: disableMetrics,
		maxAttempts:    maxAttempts,
		metricsTags:    metricsTags,
		retry:          retry,
		timeout:        timeout,
		transport:      transport,
		uris:           uris,
	}, nil
}

func mapValidParamsSnapshot(r refreshable.Refreshable, mapFn func(params validParamsSnapshot) interface{}) refreshable.Refreshable {
	return r.Map(func(i interface{}) interface{} {
		return mapFn(i.(validParamsSnapshot))
	})
}

func newDurationPtr(dur time.Duration) *time.Duration {
	return &dur
}

func derefDurationPtr(durPtr *time.Duration, defaultVal time.Duration) time.Duration {
	if durPtr == nil {
		return defaultVal
	}
	return *durPtr
}

func derefIntPtr(intPtr *int, defaultVal int) int {
	if intPtr == nil {
		return defaultVal
	}
	return *intPtr
}

func derefBoolPtr(boolPtr *bool, defaultVal bool) bool {
	if boolPtr == nil {
		return defaultVal
	}
	return *boolPtr
}

func newFloatPtr(f float64) *float64 {
	return &f
}

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
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshingclient"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	defaultDialTimeout           = 5 * time.Second
	defaultHTTPTimeout           = 60 * time.Second
	defaultKeepAlive             = 30 * time.Second
	defaultIdleConnTimeout       = 90 * time.Second
	defaultTLSHandshakeTimeout   = 10 * time.Second
	defaultExpectContinueTimeout = 1 * time.Second
	defaultMaxIdleConns          = 200
	defaultMaxIdleConnsPerHost   = 100
	defaultInitialBackoff        = 2 * time.Second
	defaultMaxBackoff            = 250 * time.Millisecond
	defaultMultiplier            = 2
	defaultRandomization         = 0.15
)

type clientBuilder struct {
	HTTP *httpClientBuilder

	URIs refreshable.StringSlice

	ErrorDecoder ErrorDecoder

	BytesBufferPool bytesbuffers.Pool
	MaxAttempts     refreshable.IntPtr // 0 means no limit. if nil, use 2*len(uris)
	RetryParams     *refreshingclient.RetryParams
}

type httpClientBuilder struct {
	ServiceName     metrics.Tag
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

func (b *httpClientBuilder) Build(ctx context.Context, params ...HTTPClientParam) (refreshingclient.RefreshableHTTPClient, error) {
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
		ServiceNameTag: b.ServiceName,
		Dialer:         refreshingclient.NewRefreshableDialer(ctx, b.DialerParams),
	}
	transport := refreshingclient.NewRefreshableTransport(ctx, b.TransportParams, dialer)
	transport = wrapTransport(transport,
		newMetricsMiddleware(b.ServiceName, b.MetricsTagProviders, b.DisableMetrics),
		traceMiddleware{CreateRequestSpan: b.CreateRequestSpan, InjectHeaders: b.InjectTraceHeaders, ServiceName: b.ServiceName.Value()},
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
	b, err := newClientBuilderFromRefreshableConfig(config)
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

// NewHTTPClientFromRefreshableConfig returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClientFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, params ...HTTPClientParam) (*http.Client, error) {
	b, err := newClientBuilderFromRefreshableConfig(config)
	if err != nil {
		return nil, err
	}
	provider, err := b.HTTP.Build(ctx, params...)
	if err != nil {
		return nil, err
	}
	return provider.CurrentHTTPClient(), nil
}

func newClientBuilder() *clientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return &clientBuilder{
		HTTP: &httpClientBuilder{
			ServiceName: metrics.Tag{},
			Timeout:     refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultHTTPTimeout)),
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
		RetryParams: &refreshingclient.RetryParams{
			InitialBackoff:      refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultInitialBackoff)),
			MaxBackoff:          refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultMaxBackoff)),
			Multiplier:          refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(defaultMultiplier))),
			RandomizationFactor: refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(defaultRandomization))),
		},
	}
}

func newClientBuilderFromRefreshableConfig(config RefreshableClientConfig) (*clientBuilder, error) {
	// Create default builder, the overwrite its refreshables with the configured ones.
	b := newClientBuilder()

	serviceNameTag, err := metrics.NewTag(MetricTagServiceName, config.ServiceName().CurrentString())
	if err != nil {
		return nil, werror.Wrap(err, "invalid service name metrics tag")
	}
	b.HTTP.ServiceName = serviceNameTag

	b.URIs = config.URIs()

	dialer, err := refreshable.MapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		c := i.(ClientConfig)
		params := refreshingclient.DialerParams{
			DialTimeout: derefDurationPtr(c.ConnectTimeout, defaultDialTimeout),
			KeepAlive:   defaultKeepAlive,
		}
		params.SocksProxyURL, err = parseOptionalProxyURLWithSchemes(c.ProxyURL, "socks5", "socks5h")
		if err != nil {
			return nil, err
		}
		return params, nil
	})
	if err != nil {
		return nil, err
	}
	b.HTTP.DialerParams = refreshingclient.RefreshableDialerParams{Refreshable: dialer}

	transport, err := refreshable.MapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		c := i.(ClientConfig)
		tlsConfig, err := newTLSConfig(c.Security)
		if err != nil {
			return nil, err
		}
		params := refreshingclient.TransportParams{
			ServiceNameTag:        serviceNameTag,
			MaxIdleConns:          derefIntPtr(c.MaxIdleConns, defaultMaxIdleConns),
			MaxIdleConnsPerHost:   derefIntPtr(c.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost),
			DisableHTTP2:          derefBoolPtr(c.DisableHTTP2, false),
			IdleConnTimeout:       derefDurationPtr(c.IdleConnTimeout, defaultIdleConnTimeout),
			ExpectContinueTimeout: derefDurationPtr(c.ExpectContinueTimeout, defaultExpectContinueTimeout),
			ProxyFromEnvironment:  derefBoolPtr(c.ProxyFromEnvironment, false),
			TLSConfig:             tlsConfig,
			TLSHandshakeTimeout:   derefDurationPtr(c.TLSHandshakeTimeout, defaultTLSHandshakeTimeout),
		}
		params.HTTPProxyURL, err = parseOptionalProxyURLWithSchemes(c.ProxyURL, "https", "http")
		if err != nil {
			return nil, err
		}
		return params, nil
	})
	if err != nil {
		return nil, err
	}
	b.HTTP.TransportParams = refreshingclient.RefreshableTransportParams{Refreshable: transport}

	b.HTTP.Timeout = refreshable.NewDuration(config.MapClientConfig(func(c ClientConfig) interface{} {
		rt := derefDurationPtr(c.ReadTimeout, 0)
		wt := derefDurationPtr(c.WriteTimeout, 0)
		// return max of read and write
		if rt > wt {
			return rt
		}
		return wt
	}))

	b.MaxAttempts = refreshable.NewIntPtr(config.MaxNumRetries().MapIntPtr(func(i *int) interface{} {
		var result *int
		if i != nil {
			v := *i + 1
			result = &v
		}
		return result
	}))

	b.RetryParams.InitialBackoff = refreshable.NewDuration(config.InitialBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, defaultInitialBackoff) // match default from retry package
	}))

	b.RetryParams.MaxBackoff = refreshable.NewDuration(config.MaxBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, defaultMaxBackoff)
	}))

	b.HTTP.DisableMetrics = refreshable.NewBool(config.Metrics().Enabled().MapBoolPtr(func(b *bool) interface{} {
		if b == nil {
			return false
		}
		return !*b
	}))

	metricTags, err := refreshable.MapValidatingRefreshable(config.Metrics().Tags(), func(i interface{}) (interface{}, error) {
		return metrics.NewTags(i.(map[string]string))
	})
	if err != nil {
		return nil, err
	}
	metricTagProvider := TagsProviderFunc(func(*http.Request, *http.Response) metrics.Tags {
		return metricTags.Current().(metrics.Tags)
	})
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders, metricTagProvider)

	return b, nil
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

func parseOptionalProxyURLWithSchemes(urlStr *string, schemes ...string) (*url.URL, error) {
	if urlStr == nil {
		return nil, nil
	}
	proxyURL, err := url.Parse(*urlStr)
	if err != nil {
		return nil, werror.Wrap(err, "invalid proxy URL")
	}
	for _, scheme := range schemes {
		if proxyURL.Scheme == scheme {
			return proxyURL, nil
		}
	}
	return nil, nil
}

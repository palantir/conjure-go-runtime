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

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal/refreshabletransport"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/pkg/retry"
	"github.com/palantir/pkg/tlsconfig"
	werror "github.com/palantir/witchcraft-go-error"
)

type clientBuilder struct {
	httpClientBuilder *httpClientBuilder

	uris refreshable.StringSlice

	errorDecoder                  ErrorDecoder
	disableTraceHeaderPropagation refreshable.Bool

	MaxAttempts refreshable.Int
	RetryParams refreshabletransport.RetryParams
}

func (b *clientBuilder) Build() (Client, error) {
	panic("implement me")
}

type httpClientBuilder struct {
	ServiceName     metrics.Tag
	Timeout         refreshable.Duration
	DialerParams    refreshabletransport.RefreshableDialerParams
	TransportParams refreshabletransport.RefreshableTransportParams
	Middlewares     []Middleware
	BytesBufferPool bytesbuffers.Pool

	DisableMetrics      refreshable.Bool
	DisableRecovery     refreshable.Bool
	DisableTracing      refreshable.Bool
	MetricsTagProviders []TagsProvider
}

func (b *httpClientBuilder) Build(ctx context.Context) refreshabletransport.RefreshableHTTPClient {
	dialer := &metricsWrappedDialer{
		Disabled:       b.DisableMetrics,
		ServiceNameTag: b.ServiceName,
		Dialer:         refreshabletransport.NewRefreshableDialer(ctx, b.DialerParams),
	}
	transport := refreshabletransport.NewRefreshableTransport(ctx, b.TransportParams, dialer)
	transport = wrapTransport(transport,
		newMetricsMiddleware(b.ServiceName, b.MetricsTagProviders, b.DisableMetrics),
		traceMiddleware{Disabled: b.DisableTracing, ServiceName: b.ServiceName.Value()},
		recoveryMiddleware{Disabled: b.DisableRecovery})
	transport = wrapTransport(transport, b.Middlewares...)

	return refreshabletransport.NewRefreshableHTTPClient(ctx, transport, b.Timeout)
}

// NewClient returns a configured client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewClient(params ...ClientParam) (Client, error) {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	b := &clientBuilder{
		httpClientBuilder: httpClientBuilder{
			TransportParams: refreshableClientParams{
				Transport: refreshableTransportParams{
					TLSClientConfig: refreshable.NewDefaultRefreshable(defaultTLSConfig),
				},
			},
		},
		backoffOptions: []retry.Option{retry.WithInitialBackoff(250 * time.Millisecond)},
		errorDecoder:   restErrorDecoder{},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.apply(b); err != nil {
			return nil, err
		}
	}
	provider, err := newRefreshableHTTPClient(context.TODO(), b.TransportParams)
	if err != nil {
		return nil, err
	}

	var edm Middleware
	if b.errorDecoder != nil {
		edm = errorDecoderMiddleware(b.errorDecoder)
	}

	return &clientImpl{
		client:                        provider,
		uris:                          b.uris,
		maxAttempts:                   b.maxAttempts,
		backoffOptions:                b.backoffOptions,
		disableTraceHeaderPropagation: b.disableTraceHeaderPropagation,
		middlewares:                   b.Middlewares,
		errorDecoderMiddleware:        edm,
		bufferPool:                    b.BytesBufferPool,
	}, nil
}

// NewHTTPClient returns a configured http client ready for use.
// We apply "sane defaults" before applying the provided params.
func NewHTTPClient(params ...HTTPClientParam) (*http.Client, error) {
	b := newClientBuilder()
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	b := &httpClientBuilder{
		TransportParams: refreshableClientParams{
			Transport: refreshableTransportParams{
				TLSClientConfig: refreshable.NewDefaultRefreshable(defaultTLSConfig),
			},
		},
	}
	for _, p := range params {
		if p == nil {
			continue
		}
		if err := p.applyHTTPClient(b); err != nil {
			return nil, err
		}
	}
	provider := b.Build(context.TODO())

	return provider.CurrentHTTPClient(), nil
}

func newClientBuilder() clientBuilder {
	defaultTLSConfig, _ := tlsconfig.NewClientConfig()
	return clientBuilder{
		httpClientBuilder: &httpClientBuilder{
			ServiceName:         metrics.Tag{},
			Timeout:             refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultHTTPTimeout)),
			DialerParams:        refreshabletransport.RefreshableDialerParams{Refreshable: refreshable.NewDefaultRefreshable(refreshabletransport.DialerParams{
				DialTimeout:   defaultDialTimeout,
				KeepAlive:     defaultKeepAlive,
				SocksProxyURL: nil,
			})},
			TransportParams:     refreshabletransport.RefreshableTransportParams{Refreshable: refreshable.NewDefaultRefreshable(refreshabletransport.TransportParams{
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
				ProxyFromEnvironment:  false,
				TLSConfig:             defaultTLSConfig,
			})},
			Middlewares:         nil,
			BytesBufferPool:     nil,
			DisableMetrics:      refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			DisableRecovery:     refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			DisableTracing:      refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			MetricsTagProviders: nil,
		},
		uris:                          nil,
		errorDecoder:                  restErrorDecoder{},
		disableTraceHeaderPropagation: refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
		MaxAttempts:                   refreshable.NewInt(refreshable.NewDefaultRefreshable(2)),
		RetryParams:                   refreshabletransport.RetryParams{
			InitialBackoff:      refreshable.NewDuration(refreshable.NewDefaultRefreshable(2*time.Second)),
			MaxBackoff:          refreshable.NewDuration(refreshable.NewDefaultRefreshable(250*time.Millisecond)),
			Multiplier:          refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(2))),
			RandomizationFactor: refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(0.15))),
		},
	}
}

func newClientBuilderFromRefreshableConfig(config RefreshableClientConfig) (*clientBuilder, error) {
	serviceNameTag, err := metrics.NewTag(MetricTagServiceName, config.ServiceName().CurrentString())
	if err != nil {
		return nil, werror.Wrap(err, "invalid service name metrics tag")
	}
	dialer, err := refreshable.MapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		c := i.(ClientConfig)
		params := refreshabletransport.DialerParams{
			DialTimeout: derefDurationPtr(c.ConnectTimeout, defaultDialTimeout),
			KeepAlive:   defaultKeepAlive,
		}
		params.SocksProxyURL, err = parseOptionalProxyUrlWithSchemes(c.ProxyURL, "socks5", "socks5h")
		if err != nil {
			return nil, err
		}
		return params, nil
	})
	if err != nil {
		return nil, err
	}

	transport, err := refreshable.MapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		c := i.(ClientConfig)
		tlsConfig, err := newTLSConfig(c.Security)
		if err != nil {
			return nil, err
		}
		params := refreshabletransport.TransportParams{
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
		params.HTTPProxyURL, err = parseOptionalProxyUrlWithSchemes(c.ProxyURL, "https", "http")
		if err != nil {
			return nil, err
		}
		return params, nil
	})
	if err != nil {
		return nil, err
	}

	timeout := refreshable.NewDuration(config.MapClientConfig(func(c ClientConfig) interface{} {
		rt := derefDurationPtr(c.ReadTimeout, 0)
		wt := derefDurationPtr(c.WriteTimeout, 0)
		// return max of read and write
		if rt > wt {
			return rt
		}
		return wt
	}))

	maxAttempts := refreshable.NewInt(config.MaxNumRetries().MapIntPtr(func(i *int) interface{} {
		if i == nil {
			return len(config.URIs().CurrentStringSlice()) * 2
		}
		return *i
	}))

	initialBackoff := refreshable.NewDuration(config.InitialBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, 2*time.Second) // match default from retry package
	}))

	maxBackoff := refreshable.NewDuration(config.MaxBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, 250*time.Millisecond)
	}))

	multiplier := refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(2)))
	randomization := refreshable.NewFloat64(refreshable.NewDefaultRefreshable(float64(0.15)))

	disableMetrics := refreshable.NewBool(config.Metrics().Enabled().MapBoolPtr(func(b *bool) interface{} {
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

	return &clientBuilder{
		uris:                          config.URIs(),
		disableTraceHeaderPropagation: refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
		errorDecoder:                  restErrorDecoder{},
		MaxAttempts:                   maxAttempts,
		httpClientBuilder: &httpClientBuilder{
			ServiceName:     serviceNameTag,
			Timeout:         timeout,
			DialerParams:    refreshabletransport.RefreshableDialerParams{Refreshable: dialer},
			TransportParams: refreshabletransport.RefreshableTransportParams{Refreshable: transport},
			DisableMetrics:  disableMetrics,
			DisableRecovery: refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			DisableTracing:  refreshable.NewBool(refreshable.NewDefaultRefreshable(false)),
			MetricsTagProviders: []TagsProvider{
				TagsProviderFunc(func(*http.Request, *http.Response) metrics.Tags {
					return metricTags.Current().(metrics.Tags)
				}),
			},
		},
		RetryParams: refreshabletransport.RetryParams{
			InitialBackoff:      initialBackoff,
			MaxBackoff:          maxBackoff,
			Multiplier:          multiplier,
			RandomizationFactor: randomization,
		},
	}, nil
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

func parseOptionalProxyUrlWithSchemes(urlStr *string, schemes ...string) (*url.URL, error) {
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

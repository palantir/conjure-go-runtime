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
	defaultInitialBackoff        = 250 * time.Millisecond
	defaultMaxBackoff            = 2 * time.Second
)

type clientBuilder struct {
	HTTP *httpClientBuilder

	URIs refreshable.StringSlice

	ErrorDecoder ErrorDecoder

	BytesBufferPool bytesbuffers.Pool
	RetryParams     refreshingclient.RetryParams
}

type httpClientBuilder struct {
	ServiceNameTag  metrics.Tag // service name is not refreshable.
	Timeout         refreshable.Duration
	DialerParams    refreshingclient.RefreshableDialerParams
	TLSConfig       *tls.Config // TODO: Make this refreshingclient.RefreshableTLSConfig and wire into transport
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
	transport := refreshingclient.NewRefreshableTransport(ctx, b.TransportParams, b.TLSConfig, dialer)
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
		backoffOptions:         b.RetryParams,
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
	if err := newClientBuilderFromRefreshableConfig(ctx, config, b, nil); err != nil {
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
			TLSConfig: defaultTLSConfig,
			TransportParams: refreshingclient.RefreshableTransportParams{Refreshable: refreshable.NewDefaultRefreshable(refreshingclient.TransportParams{
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
		RetryParams: refreshingclient.RetryParams{
			MaxAttempts:    refreshable.NewIntPtr(refreshable.NewDefaultRefreshable((*int)(nil))),
			InitialBackoff: refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultInitialBackoff)),
			MaxBackoff:     refreshable.NewDuration(refreshable.NewDefaultRefreshable(defaultMaxBackoff)),
		},
	}
}

func newClientBuilderFromRefreshableConfig(ctx context.Context, config RefreshableClientConfig, b *clientBuilder, reloadErrorSubmitter func(error)) error {
	var err error
	b.HTTP.ServiceNameTag, err = metrics.NewTag(MetricTagServiceName, config.CurrentClientConfig().ServiceName)
	if err != nil {
		return werror.WrapWithContextParams(ctx, err, "invalid service name metrics tag")
	}
	config.ServiceName().SubscribeToString(func(s string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: Service name changed but can not be live-reloaded.",
			svc1log.SafeParam("existingServiceName", b.HTTP.ServiceNameTag.Value()),
			svc1log.SafeParam("updatedServiceName", s))
	})

	//TODO: Implement refreshable TLS configuration.
	// It is hard to represent all of the configuration (e.g. a dynamic function for GetCertificate) in primitive values friendly to reflect.DeepEqual.
	currentSecurity := config.Security().CurrentSecurityConfig()
	if tlsConfig, err := newTLSConfig(currentSecurity); err != nil {
		return err
	} else if tlsConfig != nil {
		b.HTTP.TLSConfig = tlsConfig
	}
	config.Security().CAFiles().SubscribeToStringSlice(func(caFiles []string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: CAFiles configuration changed but can not be live-reloaded.",
			svc1log.SafeParam("existingCAFiles", currentSecurity.CAFiles),
			svc1log.SafeParam("ignoredCAFiles", caFiles))
	})
	config.Security().CertFile().SubscribeToString(func(certFile string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: CertFile configuration changed but can not be live-reloaded.",
			svc1log.SafeParam("existingCertFile", currentSecurity.CertFile),
			svc1log.SafeParam("ignoredCertFile", certFile))
	})
	config.Security().KeyFile().SubscribeToString(func(keyFile string) {
		svc1log.FromContext(ctx).Warn("conjure-go-runtime: KeyFile configuration changed but can not be live-reloaded.",
			svc1log.SafeParam("existingKeyFile", currentSecurity.KeyFile),
			svc1log.SafeParam("ignoredKeyFile", keyFile))
	})

	refreshingParams, err := refreshable.NewMapValidatingRefreshable(config, func(i interface{}) (interface{}, error) {
		p, err := newValidParamsSnapshot(ctx, i.(ClientConfig))
		if reloadErrorSubmitter != nil {
			reloadErrorSubmitter(err)
		}
		return p, err
	})
	if err != nil {
		return err
	}
	mapValidParamsSnapshot := func(mapFn func(params validParamsSnapshot) interface{}) refreshable.Refreshable {
		return refreshingParams.Map(func(i interface{}) interface{} {
			return mapFn(i.(validParamsSnapshot))
		})
	}
	b.HTTP.DialerParams.Refreshable = mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.Dialer
	})
	b.HTTP.TransportParams.Refreshable = mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.Transport
	})
	b.HTTP.Timeout = refreshable.NewDuration(mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.Timeout
	}))
	b.HTTP.DisableMetrics = refreshable.NewBool(mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.DisableMetrics
	}))
	b.HTTP.MetricsTagProviders = append(b.HTTP.MetricsTagProviders, refreshableMetricsTagsProvider{Refreshable: mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.MetricsTags
	})})
	b.URIs = refreshable.NewStringSlice(mapValidParamsSnapshot(func(params validParamsSnapshot) interface{} {
		return params.URIs
	}))
	b.RetryParams.MaxAttempts = config.MaxNumRetries()
	b.RetryParams.InitialBackoff = refreshable.NewDuration(config.InitialBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, defaultInitialBackoff)
	}))
	b.RetryParams.MaxBackoff = refreshable.NewDuration(config.MaxBackoff().MapDurationPtr(func(duration *time.Duration) interface{} {
		return derefDurationPtr(duration, defaultMaxBackoff)
	}))
	return nil
}

// validParamsSnapshot represents a set of fields derived from a snapshot of ClientConfig.
// It is designed for use within a refreshable: fields are comparable with reflect.DeepEqual
// so unnecessary updates are not pushed to subscribers.
// Values are generally known to be "valid" to minimize downstream error handling.
type validParamsSnapshot struct {
	Dialer         refreshingclient.DialerParams
	DisableMetrics bool
	MetricsTags    metrics.Tags
	Timeout        time.Duration
	Transport      refreshingclient.TransportParams
	URIs           []string
}

func newValidParamsSnapshot(ctx context.Context, config ClientConfig) (validParamsSnapshot, error) {
	dialer := refreshingclient.DialerParams{
		DialTimeout: derefDurationPtr(config.ConnectTimeout, defaultDialTimeout),
		KeepAlive:   defaultKeepAlive,
	}

	transport := refreshingclient.TransportParams{
		MaxIdleConns:          derefIntPtr(config.MaxIdleConns, defaultMaxIdleConns),
		MaxIdleConnsPerHost:   derefIntPtr(config.MaxIdleConnsPerHost, defaultMaxIdleConnsPerHost),
		DisableHTTP2:          derefBoolPtr(config.DisableHTTP2, false),
		IdleConnTimeout:       derefDurationPtr(config.IdleConnTimeout, defaultIdleConnTimeout),
		ExpectContinueTimeout: derefDurationPtr(config.ExpectContinueTimeout, defaultExpectContinueTimeout),
		ProxyFromEnvironment:  derefBoolPtr(config.ProxyFromEnvironment, false),
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
		Dialer:         dialer,
		DisableMetrics: disableMetrics,
		MetricsTags:    metricsTags,
		Timeout:        timeout,
		Transport:      transport,
		URIs:           uris,
	}, nil
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

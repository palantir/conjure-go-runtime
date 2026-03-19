// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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

package refreshingclient

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/http2"
)

//const (
//	defaultDialTimeout           = 10 * time.Second
//	defaultHTTPTimeout           = 60 * time.Second
//	defaultKeepAlive             = 30 * time.Second
//	defaultIdleConnTimeout       = 90 * time.Second
//	defaultTLSHandshakeTimeout   = 10 * time.Second
//	defaultExpectContinueTimeout = 1 * time.Second
//	defaultMaxIdleConns          = 200
//	defaultMaxIdleConnsPerHost   = 100
//	defaultHTTP2ReadIdleTimeout  = 30 * time.Second
//	defaultHTTP2PingTimeout      = 15 * time.Second
//	defaultInitialBackoff        = 250 * time.Millisecond
//	defaultMaxBackoff            = 2 * time.Second
//)
//
//type TransportBuilder struct {
//	internal atomic.Pointer[http.Transport]
//}
//
//var DefaultTransport http.RoundTripper = &http.Transport{
//	Proxy: http.ProxyFromEnvironment,
//	DialContext: (&net.Dialer{
//		Timeout:   30 * time.Second,
//		KeepAlive: 30 * time.Second,
//	}).DialContext,
//	ForceAttemptHTTP2:     true,
//	MaxIdleConns:          100,
//	IdleConnTimeout:       90 * time.Second,
//	TLSHandshakeTimeout:   10 * time.Second,
//	ExpectContinueTimeout: 1 * time.Second,
//}
//
//func NewTransportBuilder(ctx context.Context, dialerParams refreshable.Refreshable[DialerParams], tlsConfig refreshable.Validated[*tls.Config]) *TransportBuilder {
//	b := new(TransportBuilder)
//	dialer, tlsDialer := NewRefreshableDialers(ctx, dialerParams, tlsConfig)
//	b.internal.Store(&http.Transport{
//		Proxy:                  http.ProxyFromEnvironment,
//		OnProxyConnectResponse: nil,
//		DialContext:            dialer.DialContext,
//		Dial:                   nil,
//		DialTLSContext:         tlsDialer.DialTLSContext,
//		DialTLS:                nil,
//		TLSClientConfig:        nil,
//		TLSHandshakeTimeout:    defaultTLSHandshakeTimeout,
//		DisableKeepAlives:      false,
//		DisableCompression:     false,
//		MaxIdleConns:           defaultMaxIdleConns,
//		MaxIdleConnsPerHost:    defaultMaxIdleConnsPerHost,
//		MaxConnsPerHost:        0,
//		IdleConnTimeout:        defaultIdleConnTimeout,
//		ResponseHeaderTimeout:  0,
//		ExpectContinueTimeout:  defaultExpectContinueTimeout,
//		TLSNextProto:           nil,
//		ProxyConnectHeader:     nil,
//		GetProxyConnectHeader:  nil,
//		MaxResponseHeaderBytes: 0,
//		WriteBufferSize:        0,
//		ReadBufferSize:         0,
//		ForceAttemptHTTP2:      false,
//		HTTP2:                  nil,
//		Protocols:              nil,
//	})
//	return b
//}
//
//func (b *TransportBuilder) SetStaticHTTPProxyURL(proxy *url.URL) *TransportBuilder {
//	return b.WithParams(TransportWithProxyURL(proxy))
//}
//
//func (b *TransportBuilder) WithParams(params ...func(transport *http.Transport)) *TransportBuilder {
//	clone := b.internal.Load().Clone()
//	for _, param := range params {
//		if param != nil {
//			param(clone)
//		}
//	}
//	t := new(TransportBuilder)
//	t.internal.Store(clone)
//	return t
//}
//
//func TransportWithProxyURL(proxy *url.URL) func(transport *http.Transport) {
//	return func(transport *http.Transport) { transport.Proxy = http.ProxyURL(proxy) }
//}
//
//type Builder[S Builder[S, T], T any] interface {
//	WithParams(params ...func(T)) S
//}
//
//var _ Builder[*TransportBuilder, *http.Transport] = &TransportBuilder{}
//
//func (b *TransportBuilder) RoundTrip(req *http.Request) (*http.Response, error) {
//	return b.internal.Load().RoundTrip(req)
//}
//
//var defaults = TransportParams{
//	MaxIdleConns:          defaultMaxIdleConns,
//	MaxIdleConnsPerHost:   defaultMaxIdleConnsPerHost,
//	DisableHTTP2:          false,
//	DisableKeepAlives:     false,
//	IdleConnTimeout:       defaultIdleConnTimeout,
//	ExpectContinueTimeout: defaultExpectContinueTimeout,
//	ResponseHeaderTimeout: 0,
//	TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
//	HTTPProxyURL:          nil,
//	ProxyFromEnvironment:  true,
//	HTTP2ReadIdleTimeout:  defaultHTTP2ReadIdleTimeout,
//	HTTP2PingTimeout:      defaultHTTP2PingTimeout,
//}

type TransportParams struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
	DisableKeepAlives     bool
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	HTTPProxyURL          *url.URL
	ProxyFromEnvironment  bool
	HTTP2ReadIdleTimeout  time.Duration
	HTTP2PingTimeout      time.Duration

	TLSConfigurationParams TLSConfigurationParams
}

type TLSConfigurationParams struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
	DynamicCertReload  bool
}

func NewRefreshableTransport(ctx context.Context, p refreshable.Refreshable[TransportParams], refreshableConfig refreshable.Validated[*tls.Config], dialer ContextDialer) http.RoundTripper {
	mapped, _ := refreshable.MergeValidatedAndRefreshable(ctx, refreshableConfig, p, func(t *tls.Config, p TransportParams) *http.Transport {
		return newTransport(ctx, p, t, dialer)
	})
	return &RefreshableTransport{Refreshable: mapped}
}

// RefreshableTransport implements http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
type RefreshableTransport struct {
	Refreshable refreshable.Validated[*http.Transport]
}

func (r *RefreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.Refreshable.Unvalidated().RoundTrip(req)
}

func newTransport(ctx context.Context, p TransportParams, tlsConfig *tls.Config, dialer ContextDialer) *http.Transport {
	svc1log.FromContext(ctx).Debug("Reconstructing HTTP Transport")

	var transportProxy func(*http.Request) (*url.URL, error)
	if p.HTTPProxyURL != nil {
		transportProxy = func(*http.Request) (*url.URL, error) { return p.HTTPProxyURL, nil }
	} else if p.ProxyFromEnvironment {
		transportProxy = http.ProxyFromEnvironment
	}

	transport := &http.Transport{
		Proxy:                 transportProxy,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       tlsConfig,
		DisableKeepAlives:     p.DisableKeepAlives,
		ExpectContinueTimeout: p.ExpectContinueTimeout,
		IdleConnTimeout:       p.IdleConnTimeout,
		TLSHandshakeTimeout:   p.TLSHandshakeTimeout,
		ResponseHeaderTimeout: p.ResponseHeaderTimeout,
	}

	if !p.DisableHTTP2 {
		// Attempt to configure net/http HTTP/1 Transport to use HTTP/2.
		http2Transport, err := http2.ConfigureTransports(transport)
		if err != nil {
			// ConfigureTransport's only error as of this writing is the idempotent "protocol https already registered."
			// It should never happen in our usage because this is immediately after creation.
			// In case of something unexpected, log it and move on.
			svc1log.FromContext(ctx).Error("failed to configure transport for http2", svc1log.Stacktrace(err))
		} else {
			// ReadIdleTimeout is the amount of time to wait before running periodic health checks (pings)
			// after not receiving a frame from the HTTP/2 connection.
			// Setting this value will enable the health checks and allows broken idle
			// connections to be pruned more quickly, preventing the client from
			// attempting to re-use connections that will no longer work.
			// ref: https://github.com/golang/go/issues/36026
			http2Transport.ReadIdleTimeout = p.HTTP2ReadIdleTimeout

			// PingTimeout configures the amount of time to wait for a ping response (health check)
			// before closing an HTTP/2 connection. The PingTimeout is only valid if
			// the above ReadIdleTimeout is > 0.
			http2Transport.PingTimeout = p.HTTP2PingTimeout
		}
	}

	return transport
}

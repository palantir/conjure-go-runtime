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

package refreshabletransport

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/http2"
)

type TransportParams struct {
	ServiceNameTag        metrics.Tag
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
	TLSConfig             *tls.Config
}

func NewRefreshableTransport(ctx context.Context, p RefreshableTransportParams, dialer ContextDialer) http.RoundTripper {
	return &RefreshableTransport{
		Refreshable: p.Map(func(i interface{}) interface{} {
			return newTransport(ctx, i.(TransportParams), dialer)
		}),
	}
}

type RefreshableTransportParams struct {
	refreshable.Refreshable // contains TransportParams
}

func (r RefreshableTransportParams) CurrentDialerParams() TransportParams {
	return r.Current().(TransportParams)
}

// TransformParams accepts a mapping function which will be applied to the params value as it is evaluated.
// This can be used to layer/overwrite configuration before building the RefreshableTransport.
func (r RefreshableTransportParams) TransformParams(mapFn func(p TransportParams) TransportParams) RefreshableTransportParams {
	return RefreshableTransportParams{
		Refreshable: r.Map(func(i interface{}) interface{} {
			return mapFn(i.(TransportParams))
		}),
	}
}

// RefreshableTransport implements http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
type RefreshableTransport struct {
	refreshable.Refreshable // contains *http.Transport
}

func (r RefreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.Current().(*http.Transport).RoundTrip(req)
}

func newTransport(ctx context.Context, p TransportParams, dialer ContextDialer) *http.Transport {
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       p.TLSConfig,
		DisableKeepAlives:     p.DisableKeepAlives,
		ExpectContinueTimeout: p.ExpectContinueTimeout,
		IdleConnTimeout:       p.IdleConnTimeout,
		TLSHandshakeTimeout:   p.TLSHandshakeTimeout,
		ResponseHeaderTimeout: p.ResponseHeaderTimeout,
	}

	if p.HTTPProxyURL != nil {
		transport.Proxy = func(*http.Request) (*url.URL, error) { return p.HTTPProxyURL, nil }
	} else if p.ProxyFromEnvironment {
		transport.Proxy = http.ProxyFromEnvironment
	}

	if !p.DisableHTTP2 {
		if err := http2.ConfigureTransport(transport); err != nil {
			svc1log.FromContext(ctx).Error("failed to configure transport for http2", svc1log.Stacktrace(err))
		}
	}
	return transport
}

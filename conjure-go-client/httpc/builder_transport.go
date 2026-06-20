// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc

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

// TransportBuilder configures HTTP transport settings (connection pooling,
// timeouts, HTTP/2, HTTP(S) proxy) and is one slice of [BuilderAPI].
//
// A configured *Builder can produce a standalone [http.RoundTripper] via
// [Builder.BuildTransport], or provide one for a full [Client] via
// [Builder.Build]. [TransportBuilder.SetTransport] short-circuits transport
// construction and uses the caller-provided RoundTripper instead — useful for
// testing (e.g. an httptest recorder) or production cases that need a custom
// transport (custom auth, instrumentation, etc.).
type TransportBuilder[Self TransportBuilder[Self]] interface {
	Clone() Self
	Apply(...Param[Self]) Self

	// SetMaxIdleConns sets the maximum total idle connections across hosts. Default: 200.
	SetMaxIdleConns(int) Self
	// SetMaxIdleConnsPerHost sets the maximum idle connections per host. Default: 100.
	SetMaxIdleConnsPerHost(int) Self
	// DisableKeepAlives forces a new TCP connection per request.
	DisableKeepAlives() Self
	// SetIdleConnTimeout sets how long idle pool connections live. Default: 90s.
	SetIdleConnTimeout(time.Duration) Self
	// SetExpectContinueTimeout sets the wait for a 100 Continue response. Default: 1s.
	SetExpectContinueTimeout(time.Duration) Self
	// SetResponseHeaderTimeout sets the response-headers read timeout. Default: 0 (no timeout).
	SetResponseHeaderTimeout(time.Duration) Self
	// SetTLSHandshakeTimeout sets the TLS handshake timeout. Default: 10s.
	SetTLSHandshakeTimeout(time.Duration) Self
	// DisableHTTP2 forces HTTP/1.1.
	DisableHTTP2() Self
	// SetHTTP2ReadIdleTimeout sets the idle interval before pinging an HTTP/2 connection. Default: 30s.
	SetHTTP2ReadIdleTimeout(time.Duration) Self
	// SetHTTP2PingTimeout sets the timeout for HTTP/2 ping responses. Default: 15s.
	SetHTTP2PingTimeout(time.Duration) Self
	// SetHTTPProxyURL sets an http(s):// proxy URL; "" clears it. SOCKS goes on DialerBuilder.
	SetHTTPProxyURL(string) Self
	// SetNoProxy clears all proxy configuration (HTTP, SOCKS, and environment).
	SetNoProxy() Self
	// SetProxyFromEnvironment configures the proxy from HTTP_PROXY, HTTPS_PROXY, NO_PROXY.
	SetProxyFromEnvironment() Self

	// SetTransport installs a caller-provided RoundTripper.
	// [TransportBuilder.BuildTransport] returns it as-is, bypassing the
	// dialer / TLS / transport construction path. The middleware stack still
	// wraps the transport when used through [Builder.Build]. Pass nil to
	// re-enable internal construction.
	SetTransport(http.RoundTripper) Self
	// BuildTransport returns the configured RoundTripper. If
	// [TransportBuilder.SetTransport] was called with a non-nil value, that
	// transport is returned as-is and the dialer / TLS / transport settings
	// are ignored. Otherwise a transport is built from those settings and
	// rebuilds when transport, TLS, or dialer parameters change. The returned
	// value has no middleware wrapping; use [Builder.Build] for the full stack.
	BuildTransport(ctx context.Context) (http.RoundTripper, error)
}

// SetMaxIdleConns sets the maximum total idle connections across hosts. Default: 200.
func (b *BuilderCore[Self]) SetMaxIdleConns(n int) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.MaxIdleConns = n
		return p
	})
	return b.self
}

// SetMaxIdleConnsPerHost sets the maximum number of idle connections per host. Default: 100.
func (b *BuilderCore[Self]) SetMaxIdleConnsPerHost(n int) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.MaxIdleConnsPerHost = n
		return p
	})
	return b.self
}

// DisableKeepAlives disables HTTP keep-alive connections, forcing a new connection per request.
func (b *BuilderCore[Self]) DisableKeepAlives() Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.DisableKeepAlives = true
		return p
	})
	return b.self
}

// SetIdleConnTimeout sets how long idle connections remain in the pool before closing. Default: 90s.
func (b *BuilderCore[Self]) SetIdleConnTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.IdleConnTimeout = d
		return p
	})
	return b.self
}

// SetExpectContinueTimeout sets the timeout for waiting for a 100 Continue response. Default: 1s.
func (b *BuilderCore[Self]) SetExpectContinueTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ExpectContinueTimeout = d
		return p
	})
	return b.self
}

// SetResponseHeaderTimeout sets the timeout for reading response headers. Default: 0 (no timeout).
func (b *BuilderCore[Self]) SetResponseHeaderTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ResponseHeaderTimeout = d
		return p
	})
	return b.self
}

// SetTLSHandshakeTimeout sets the timeout for the TLS handshake. Default: 10s.
func (b *BuilderCore[Self]) SetTLSHandshakeTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.TLSHandshakeTimeout = d
		return p
	})
	return b.self
}

// DisableHTTP2 disables HTTP/2 support, forcing HTTP/1.1.
func (b *BuilderCore[Self]) DisableHTTP2() Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.DisableHTTP2 = true
		return p
	})
	return b.self
}

// SetHTTP2ReadIdleTimeout sets the idle interval after which an HTTP/2 connection is health-checked
// with a ping. Default: 30s.
func (b *BuilderCore[Self]) SetHTTP2ReadIdleTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTP2ReadIdleTimeout = d
		return p
	})
	return b.self
}

// SetHTTP2PingTimeout sets the timeout for HTTP/2 ping health checks. Default: 15s.
// Only meaningful when HTTP2ReadIdleTimeout > 0.
func (b *BuilderCore[Self]) SetHTTP2PingTimeout(d time.Duration) Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTP2PingTimeout = d
		return p
	})
	return b.self
}

// SetHTTPProxyURL sets an HTTP/HTTPS proxy URL for requests. Pass "" to clear.
// Use SetSocksProxyURL for socks5:// proxies. An invalid URL or scheme defers an
// error to Build; setting a valid URL (or clearing it) replaces any prior error
// for this field.
func (b *BuilderCore[Self]) SetHTTPProxyURL(s string) Self {
	b.errs.clearField(fieldHTTPProxy)
	if s == "" {
		b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
			p.HTTPProxyURL = nil
			return p
		})
		return b.self
	}
	proxyURL, err := parseProxyURL(s, "HTTP proxy URL", "http", "https")
	if err != nil {
		b.errs.setField(fieldHTTPProxy, err)
		return b.self
	}
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTPProxyURL = proxyURL
		return p
	})
	return b.self
}

// SetNoProxy clears all proxy configuration (HTTP, SOCKS, and environment),
// along with any deferred HTTP or SOCKS proxy validation errors.
func (b *BuilderCore[Self]) SetNoProxy() Self {
	b.errs.clearField(fieldHTTPProxy, fieldSocksProxy)
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.SocksProxyURL = nil
		return p
	})
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.HTTPProxyURL = nil
		p.ProxyFromEnvironment = false
		return p
	})
	return b.self
}

// SetProxyFromEnvironment configures the proxy from HTTP_PROXY, HTTPS_PROXY, and NO_PROXY
// environment variables.
func (b *BuilderCore[Self]) SetProxyFromEnvironment() Self {
	b.transportParams = refreshable.View(b.transportParams, func(p transportParams) transportParams {
		p.ProxyFromEnvironment = true
		return p
	})
	return b.self
}

type transportParams struct {
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
}

// refreshableTransport is an http.RoundTripper that rebuilds its underlying
// *http.Transport when transport, TLS, or dialer parameters change.
type refreshableTransport struct {
	Refreshable refreshable.Validated[*http.Transport]
}

func (r *refreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.Refreshable.Unvalidated().RoundTrip(req)
}

// CloseIdleConnections releases idle connections on the live *http.Transport,
// preserving the standard optional capability through this wrapper.
func (r *refreshableTransport) CloseIdleConnections() {
	r.Refreshable.Unvalidated().CloseIdleConnections()
}

func newTransport(ctx context.Context, p transportParams, tlsConfig *tls.Config, dialer ContextDialer) *http.Transport {
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
		http2Transport, err := http2.ConfigureTransports(transport)
		if err != nil {
			// ConfigureTransports' only documented error is the idempotent
			// "protocol https already registered" — log defensively and continue.
			svc1log.FromContext(ctx).Error("failed to configure transport for http2", svc1log.Stacktrace(err))
		} else {
			// Setting ReadIdleTimeout enables periodic pings so broken idle HTTP/2
			// connections are pruned (golang/go#36026). PingTimeout applies only
			// when ReadIdleTimeout > 0.
			http2Transport.ReadIdleTimeout = p.HTTP2ReadIdleTimeout
			http2Transport.PingTimeout = p.HTTP2PingTimeout
		}
	}

	return transport
}

// BuildTransport returns the configured http.RoundTripper. If
// [Builder.SetTransport] was called with a non-nil value, that transport is
// returned as-is and the dialer / TLS / transport settings are ignored.
// Otherwise the transport is built and rebuilds when transport, TLS, or dialer
// parameters change. The returned value has no middleware wrapping; use
// [Builder.BuildHTTPClient] or [Builder.Build] for the full stack.
func (b *BuilderCore[Self]) BuildTransport(ctx context.Context) (http.RoundTripper, error) {
	if b.transport != nil {
		// The override replaces the constructed transport; proxy errors are moot.
		if err := b.errs.joined(ctx, fieldConfig); err != nil {
			return nil, err
		}
		return b.transport, nil
	}
	// SOCKS proxy errors surface via BuildDialer below.
	if err := b.errs.joined(ctx, fieldConfig, fieldHTTPProxy); err != nil {
		return nil, err
	}
	tlsConfig, err := b.BuildTLSConfig(ctx)
	if err != nil {
		return nil, err
	}
	dialer, err := b.BuildDialer(ctx)
	if err != nil {
		return nil, err
	}
	var prev *http.Transport
	mapped := refreshable.MergeValidatedAndRefreshableAuto(ctx, tlsConfig, b.transportParams, func(t *tls.Config, p transportParams) *http.Transport {
		next := newTransport(ctx, p, t, dialer)
		if prev != nil {
			svc1log.FromContext(ctx).Debug("Reconstructing HTTP Transport")
			// Release idle connections pooled on the transport being retired;
			// in-flight requests on it are unaffected (only idle conns close).
			prev.CloseIdleConnections()
		}
		prev = next
		return next
	})
	return &refreshableTransport{Refreshable: mapped}, nil
}

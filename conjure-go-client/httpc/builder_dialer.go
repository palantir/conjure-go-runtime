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
	"net"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/proxy"
)

// DialerBuilder configures TCP dialer settings (dial timeout, keep-alive,
// SOCKS proxy) and is one slice of [ClientBuilder]. A configured *Builder can
// produce a standalone [ContextDialer] via [Builder.BuildDialer], or provide
// one for a full [Client] via [Builder.Build].
type DialerBuilder[B DialerBuilder[B]] interface {
	Clone() B
	Apply(...Param[B]) B

	// SetDialTimeout sets the maximum duration for establishing a TCP connection. Default: 10s.
	SetDialTimeout(time.Duration) B
	// SetKeepAlive sets the interval between TCP keep-alive probes. Default: 30s.
	SetKeepAlive(time.Duration) B
	// SetSocksProxyURL sets a socks5:// proxy URL. Pass "" to clear. Use
	// TransportBuilder.SetHTTPProxyURL for http(s) proxies.
	SetSocksProxyURL(string) B

	// SetDialer installs a caller-provided dialer. [DialerBuilder.BuildDialer]
	// returns it as-is, skipping construction from SetDialTimeout/SetKeepAlive/
	// SetSocksProxyURL. Pass nil to clear and re-enable internal construction.
	SetDialer(ContextDialer) B
	// BuildDialer returns the configured dialer. If [DialerBuilder.SetDialer]
	// was called with a non-nil dialer, it is returned directly and the
	// SetDialTimeout/SetKeepAlive/SetSocksProxyURL settings are ignored.
	// Otherwise a fresh dialer is built from those settings and rebuilds
	// automatically when any refreshable source changes.
	BuildDialer(ctx context.Context) (ContextDialer, error)
}

// SetDialer installs a caller-provided dialer. [Builder.BuildDialer] (and the
// transport built by [Builder.Build]) returns it as-is, skipping the internal
// SetDialTimeout/SetKeepAlive/SetSocksProxyURL construction path. Pass nil to
// re-enable internal construction.
func (b *Builder) SetDialer(d ContextDialer) *Builder {
	b.dialerOverride = d
	return b
}

// SetDialTimeout sets the maximum duration for establishing a TCP connection. Default: 10s.
func (b *Builder) SetDialTimeout(d time.Duration) *Builder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.DialTimeout = d
		return p
	})
	return b
}

// SetKeepAlive sets the interval between TCP keep-alive probes. Default: 30s.
func (b *Builder) SetKeepAlive(d time.Duration) *Builder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.KeepAlive = d
		return p
	})
	return b
}

// SetSocksProxyURL sets a SOCKS5 proxy URL for TCP connections. Pass "" to clear.
// Only socks5:// URLs are supported; use SetHTTPProxyURL for http(s) proxies.
func (b *Builder) SetSocksProxyURL(s string) *Builder {
	if s == "" {
		b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
			p.SocksProxyURL = nil
			return p
		})
		return b
	}
	proxyURL, err := url.Parse(s)
	if err != nil {
		b.errs = append(b.errs, werror.Wrap(err, "failed to parse SOCKS proxy URL"))
		return b
	}
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.SocksProxyURL = proxyURL
		return p
	})
	return b
}

// ContextDialer is the dialer interface returned by [Builder.BuildDialer];
// implemented by net.Dialer and golang.org/x/net/proxy.Dialer.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Dial(network, address string) (net.Conn, error)
}

type dialerParams struct {
	DialTimeout   time.Duration
	KeepAlive     time.Duration
	SocksProxyURL *url.URL
}

type refreshableDialer struct {
	refreshable.Refreshable[ContextDialer]
}

func (r *refreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.Current().DialContext(ctx, network, address)
}

func (r *refreshableDialer) Dial(network, address string) (net.Conn, error) {
	return r.Current().DialContext(context.TODO(), network, address)
}

// BuildDialer returns the configured dialer. If [Builder.SetDialer] was
// called with a non-nil value, that dialer is returned as-is and the
// SetDialTimeout/SetKeepAlive/SetSocksProxyURL settings are ignored.
// Otherwise the dialer is built from those settings and rebuilds
// automatically when any refreshable source changes.
func (b *Builder) BuildDialer(ctx context.Context) (ContextDialer, error) {
	if err := builderErrors(ctx, b.errs); err != nil {
		return nil, err
	}
	if b.dialerOverride != nil {
		return b.dialerOverride, nil
	}
	rebuild := false
	mapped := refreshable.MapAuto(b.dialerParams, func(p dialerParams) ContextDialer {
		if rebuild {
			svc1log.FromContext(ctx).Debug("Reconstructing HTTP Dialer")
		} else {
			rebuild = true
		}
		dialer := &net.Dialer{
			Timeout:   p.DialTimeout,
			KeepAlive: p.KeepAlive,
		}
		if p.SocksProxyURL == nil {
			return dialer
		}
		proxyDialer, err := proxy.FromURL(p.SocksProxyURL, dialer)
		if err != nil {
			// Unreachable: the SOCKS URL is validated in SetSocksProxyURL.
			svc1log.FromContext(ctx).Error("Failed to construct socks5 dialer. Please report this as a bug in conjure-go-runtime.", svc1log.Stacktrace(err))
			return dialer
		}
		// proxy.Dialer has only Dial; cast to ContextDialer if the impl supports it.
		if contextDialer, ok := proxyDialer.(ContextDialer); ok {
			return contextDialer
		}
		return dialer
	})
	return &refreshableDialer{Refreshable: mapped}, nil
}

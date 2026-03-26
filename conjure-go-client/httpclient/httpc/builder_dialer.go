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
	"errors"
	"net"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/proxy"
)

// DialerBuilder is an F-bounded interface for configuring TCP dialer settings.
//
// Like all builders in this package, DialerBuilder is mutable: setter methods modify
// the receiver and return it for chaining. Use Clone to fork an independent copy.
//
// Generic functions can accept any DialerBuilder and return the same concrete type:
//
//	func ConfigureDialer[B DialerBuilder[B]](b B) B {
//	    return b.SetDialTimeout(5 * time.Second).SetKeepAlive(15 * time.Second)
//	}
type DialerBuilder[B DialerBuilder[B]] interface {
	// Clone returns a deep copy of the builder. The copy is fully independent:
	// mutations to either the original or the clone do not affect the other.
	Clone() B

	// Apply applies the given Param functions to the builder in sequence.
	// Each Param may call setter methods to configure the builder.
	// Because builders are mutable, this modifies the receiver in place.
	Apply(...Param[B]) B

	// SetDialTimeout sets the maximum duration for establishing a TCP connection.
	// Default: 10s.
	SetDialTimeout(time.Duration) B

	// SetKeepAlive sets the interval between TCP keep-alive probes.
	// Default: 30s.
	SetKeepAlive(time.Duration) B

	// SetSocksProxyURL sets a SOCKS5 proxy URL for TCP connections.
	// Pass "" to clear. Only socks5:// URLs are supported.
	// HTTP/HTTPS proxy configuration is on TransportBuilder.
	SetSocksProxyURL(string) B
}

func (b *Builder) SetDialTimeout(d time.Duration) *Builder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.DialTimeout = d
		return p
	})
	return b
}

func (b *Builder) SetKeepAlive(d time.Duration) *Builder {
	b.dialerParams = refreshable.View(b.dialerParams, func(p dialerParams) dialerParams {
		p.KeepAlive = d
		return p
	})
	return b
}

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

// ContextDialer is the interface implemented by net.Dialer, proxy.Dialer, and others.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Dial(network, address string) (net.Conn, error)
}

// dialerParams holds the parameters needed to build a TCP dialer.
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

// BuildDialer builds the configured TCP dialer from the builder's dialer parameters.
// The returned ContextDialer automatically adapts when dial timeout, keep-alive,
// or SOCKS proxy settings change via their refreshable sources.
func (b *Builder) BuildDialer(ctx context.Context) (ContextDialer, error) {
	if len(b.errs) > 0 {
		if len(b.errs) == 1 {
			return nil, werror.WrapWithContextParams(ctx, b.errs[0], "builder configuration errors")
		}
		return nil, werror.WrapWithContextParams(ctx, errors.Join(b.errs...), "builder configuration errors")
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
			// should never happen; checked in the validating refreshable
			svc1log.FromContext(ctx).Error("Failed to construct socks5 dialer. Please report this as a bug in conjure-go-runtime.", svc1log.Stacktrace(err))
			return dialer
		}
		// proxy.Dialer interface only has Dial(), but we need DialContext.
		// The underlying dialer already implements DialContext, so we can safely cast if it's *net.Dialer.
		if contextDialer, ok := proxyDialer.(ContextDialer); ok {
			return contextDialer
		}
		return dialer
	})
	return &refreshableDialer{Refreshable: mapped}, nil
}

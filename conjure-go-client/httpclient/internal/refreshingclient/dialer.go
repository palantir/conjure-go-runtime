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
	"net"
	"net/url"
	"time"

	"github.com/palantir/pkg/refreshable"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/proxy"
)

type DialerParams struct {
	DialTimeout   time.Duration
	KeepAlive     time.Duration
	SocksProxyURL *url.URL `refreshables:",exclude"`
}

// ContextDialer is the interface implemented by net.Dialer, proxy.Dialer, and others
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

func NewRefreshableDialer(ctx context.Context, p RefreshableDialerParams) ContextDialer {
	rebuild := false
	return &RefreshableDialer{
		Refreshable: p.MapDialerParams(func(p DialerParams) interface{} {
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
			return proxyDialer
		}),
	}
}

type RefreshableDialer struct {
	refreshable.Refreshable // contains ContextDialer
}

func (r *RefreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.Current().(ContextDialer).DialContext(ctx, network, address)
}

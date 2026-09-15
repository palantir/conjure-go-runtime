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
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
)

type DialerParams struct {
	DialTimeout time.Duration
	KeepAlive   time.Duration
}

// ContextDialer is the interface implemented by net.Dialer and other context-aware dialers.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

func NewRefreshableDialer(ctx context.Context, r refreshable.Refreshable[DialerParams]) ContextDialer {
	rebuild := false
	return &RefreshableDialer{Refreshable: refreshable.MapAuto(r, func(p DialerParams) ContextDialer {
		if !rebuild {
			rebuild = true
		} else {
			svc1log.FromContext(ctx).Debug("Reconstructing HTTP Dialer")
		}
		return &net.Dialer{
			Timeout:   p.DialTimeout,
			KeepAlive: p.KeepAlive,
		}
	})}
}

type RefreshableDialer struct {
	refreshable.Refreshable[ContextDialer]
}

func (r *RefreshableDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return r.Current().DialContext(ctx, network, address)
}

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

package httpclient

import (
	"fmt"
	"net/http"
	"time"
)

func (b *httpClientBuilder) handleIdleConnUpdate(c *clientImpl) {
	b.IdleConnTimeout.SubscribeToDuration(func(v time.Duration) {
		t := unwrapTransport(c.client.Transport)
		t.IdleConnTimeout = v
		c.client.Transport = t
	})
}

func (b *httpClientBuilder) handleTLSHandshakeTimeoutUpdate(c *clientImpl) {
	b.TLSHandshakeTimeout.SubscribeToDuration(func(v time.Duration) {
		t := unwrapTransport(c.client.Transport)
		t.TLSHandshakeTimeout = v
		c.client.Transport = t
	})
}

func (b *httpClientBuilder) handleExpectContinueTimeoutUpdate(c *clientImpl) {
	b.ExpectContinueTimeout.SubscribeToDuration(func(v time.Duration) {
		t := unwrapTransport(c.client.Transport)
		t.ExpectContinueTimeout = v
		c.client.Transport = t
	})
}

func (b *httpClientBuilder) handleMaxIdleConnsUpdate(c *clientImpl) {
	b.MaxIdleConns.SubscribeToInt(func(v int) {
		t := unwrapTransport(c.client.Transport)
		t.MaxIdleConns = v
		c.client.Transport = t
	})
}

func (b *httpClientBuilder) handleMaxIdleConnsPerHostUpdate(c *clientImpl) {
	b.MaxIdleConnsPerHost.SubscribeToInt(func(v int) {
		t := unwrapTransport(c.client.Transport)
		t.MaxIdleConnsPerHost = v
		c.client.Transport = t
	})
}

func unwrapTransport(rt http.RoundTripper) *http.Transport {
	unwrapped := rt
	for {
		switch v := unwrapped.(type) {
		case *wrappedClient:
			unwrapped = v.baseTransport
		case *http.Transport:
			return v
		default:
			panic(fmt.Sprintf("unknown roundtripper type %T", unwrapped))
		}
	}
}

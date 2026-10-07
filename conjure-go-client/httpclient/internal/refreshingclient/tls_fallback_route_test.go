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

package refreshingclient

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTLSFallbackRoute(t *testing.T) {
	fallbackTransport := &http.Transport{}

	t.Run("canonical origin", func(t *testing.T) {
		transport := &managedTransport{transport: &http.Transport{}, tls12FallbackTransport: fallbackTransport}
		first := transport.tlsFallbackRoute(newRequest(t, "https://EXAMPLE.com/path"))
		require.NotNil(t, first)
		second := transport.tlsFallbackRoute(newRequest(t, "https://example.com:443/other"))
		require.NotNil(t, second)
		require.Equal(t, first, second)
		require.Equal(t, "example.com:443", first.originAuthority)
	})

	t.Run("proxy is part of route", func(t *testing.T) {
		proxyURL, err := url.Parse("http://user:password@PROXY.example.com:8080")
		require.NoError(t, err)
		transport := &managedTransport{
			transport:              &http.Transport{Proxy: http.ProxyURL(proxyURL)},
			tls12FallbackTransport: fallbackTransport,
		}
		route := transport.tlsFallbackRoute(newRequest(t, "https://example.com"))
		require.NotNil(t, route)
		require.Equal(t, "http", route.proxyScheme)
		require.Equal(t, "proxy.example.com:8080", route.proxyAuthority)
		require.NotContains(t, route.proxyAuthority, "password")
	})

	t.Run("direct and proxy routes differ", func(t *testing.T) {
		direct := &managedTransport{transport: &http.Transport{}, tls12FallbackTransport: fallbackTransport}
		proxied := &managedTransport{
			transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{
				Scheme: "http",
				Host:   "proxy.example.com",
			})},
			tls12FallbackTransport: fallbackTransport,
		}
		directRoute := direct.tlsFallbackRoute(newRequest(t, "https://example.com"))
		proxyRoute := proxied.tlsFallbackRoute(newRequest(t, "https://example.com"))
		require.NotNil(t, directRoute)
		require.NotNil(t, proxyRoute)
		require.NotEqual(t, directRoute, proxyRoute)
	})
}

func newRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	require.NoError(t, err)
	return req
}

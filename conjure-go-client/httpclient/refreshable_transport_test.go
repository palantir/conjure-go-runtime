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

package httpclient_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"sync"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientProxyRefresh(t *testing.T) {
	for _, override := range []bool{false, true} {
		t.Run(fmt.Sprint("override=", override), func(t *testing.T) {
			config := httpclient.ClientConfig{ServiceName: "test", ProxyURL: new("socks5://127.0.0.1:12345"), ProxyFromEnvironment: new(false)}
			configs := refreshable.New(config)
			var params []httpclient.HTTPClientParam
			if override {
				params = append(params, httpclient.WithNoProxy())
			}
			clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configs, params...)
			require.NoError(t, err)
			initial := unwrapTransport(clients.Current().Transport)
			config.ConnectTimeout = new(time.Second)
			configs.Update(config)
			require.Same(t, initial, unwrapTransport(clients.Current().Transport))
			for _, proxyURL := range []*string{new("socks5://127.0.0.1:12346"), new("socks5h://user:pass@proxy:1080"), new("http://proxy:8080"), new("https://proxy:8443"), nil} {
				config.ProxyURL = proxyURL
				configs.Update(config)
				current := unwrapTransport(clients.Current().Transport)
				if override {
					require.Same(t, initial, current)
					require.Nil(t, current.Proxy)
				} else {
					require.NotSame(t, initial, current)
					if proxyURL == nil {
						require.Nil(t, current.Proxy)
					} else {
						require.NotNil(t, current.Proxy)
						selected, err := current.Proxy(&http.Request{})
						require.NoError(t, err)
						require.Equal(t, *proxyURL, selected.String())
					}
				}
				initial = current
			}
		})
	}
}

func TestHTTPClientProxyURLOverrides(t *testing.T) {
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			proxyURL := scheme + "://user:pass@proxy:1080"
			config := refreshable.New(httpclient.ClientConfig{ServiceName: "test", ProxyURL: new("socks5://old-proxy:1080")})
			clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), config,
				httpclient.WithProxyURL("http://other-proxy:8080"),
				httpclient.WithProxyURL(proxyURL))
			require.NoError(t, err)
			transport := unwrapTransport(clients.Current().Transport)
			req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
			require.NoError(t, err)
			selected, err := transport.Proxy(req)
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, proxyURL, selected.String())
			config.Update(httpclient.ClientConfig{ServiceName: "test", ProxyURL: new("http://updated-proxy:8080")})
			require.Same(t, transport, unwrapTransport(clients.Current().Transport))
		})
	}
}

func TestHTTPClientCloseIdleConnections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	t.Cleanup(server.Close)
	config := httpclient.ClientConfig{ServiceName: "test"}
	configs := refreshable.New(config)
	clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configs, httpclient.WithNoProxy())
	require.NoError(t, err)
	client := clients.Current()
	t.Cleanup(client.CloseIdleConnections)
	request := func() bool {
		var reused bool
		ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		return reused
	}
	for i := range 2 {
		require.False(t, request())
		require.True(t, request())
		client.CloseIdleConnections()
		require.False(t, request())
		config.MaxIdleConns = new(50 + i)
		configs.Update(config)
	}
}

func TestHTTPClientConcurrentTransportRefresh(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)

	config := httpclient.ClientConfig{
		ServiceName:  "test",
		URIs:         []string{server.URL},
		MaxIdleConns: new(50),
		Security: httpclient.SecurityConfig{
			InsecureSkipVerify: new(true),
		},
	}
	configRefreshable := refreshable.New(config)
	clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configRefreshable, httpclient.WithNoProxy())
	require.NoError(t, err)
	client := clients.Current()
	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	require.Equal(t, 2, resp.ProtoMajor)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	start := make(chan struct{})
	var ready, wg sync.WaitGroup
	ready.Add(8)
	for range 8 {
		wg.Go(func() {
			ready.Done()
			<-start
			for range 50 {
				resp, err := client.Get(server.URL)
				if err != nil {
					t.Error(err)
					return
				}
				if _, err := io.Copy(io.Discard, resp.Body); err != nil {
					t.Error(err)
				}
				if err := resp.Body.Close(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	ready.Wait()
	close(start)
	for i := range 200 {
		next := config
		next.MaxIdleConns = new(50 + i%2)
		next.Security.DynamicCertReload = new(i%2 == 0)
		configRefreshable.Update(next)
	}
	wg.Wait()

	resp, err = client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

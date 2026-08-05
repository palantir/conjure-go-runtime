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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestHTTPClientConcurrentTransportRefresh(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	t.Cleanup(server.Close)

	config := httpclient.ClientConfig{
		ServiceName:  "test",
		URIs:         []string{server.URL},
		MaxIdleConns: new(50),
	}
	configRefreshable := refreshable.New(config)
	clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configRefreshable, httpclient.WithNoProxy())
	require.NoError(t, err)
	client := clients.Current()
	t.Cleanup(client.CloseIdleConnections)

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				resp, err := client.Get(server.URL)
				if err != nil {
					continue
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}

	for i := range 200 {
		next := config
		next.MaxIdleConns = new(50 + i%2)
		configRefreshable.Update(next)
	}
	close(stop)
	wg.Wait()

	resp, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestHTTPClientRefreshClosesRetiredIdleConnections(t *testing.T) {
	connections := &connectionStates{states: make(map[net.Conn]http.ConnState)}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = connections.update
	server.Start()
	t.Cleanup(server.Close)

	config := httpclient.ClientConfig{
		ServiceName:  "test",
		URIs:         []string{server.URL},
		MaxIdleConns: new(50),
	}
	configRefreshable := refreshable.New(config)
	clients, err := httpclient.NewHTTPClientFromRefreshableConfig(t.Context(), configRefreshable, httpclient.WithNoProxy())
	require.NoError(t, err)
	client := clients.Current()
	t.Cleanup(client.CloseIdleConnections)

	request := func() {
		resp, err := client.Get(server.URL)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	request()
	require.Eventually(t, func() bool {
		return connections.count(http.StateIdle) == 1
	}, 2*time.Second, 10*time.Millisecond)

	next := config
	next.MaxIdleConns = new(51)
	configRefreshable.Update(next)
	require.Eventually(t, func() bool {
		return connections.open() == 0
	}, 2*time.Second, 10*time.Millisecond)

	request()
	require.Eventually(t, func() bool {
		return connections.count(http.StateIdle) == 1
	}, 2*time.Second, 10*time.Millisecond)
}

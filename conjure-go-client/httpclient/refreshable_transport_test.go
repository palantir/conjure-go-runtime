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
		}()
	}

	for i := range 200 {
		next := config
		next.MaxIdleConns = new(50 + i%2)
		next.Security.DynamicCertReload = new(i%2 == 0)
		configRefreshable.Update(next)
	}
	close(stop)
	wg.Wait()

	resp, err = client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestHTTPClientRefreshClosesRetiredIdleConnections(t *testing.T) {
	connections := &refreshConnectionStates{states: make(map[net.Conn]http.ConnState)}
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

type refreshConnectionStates struct {
	mu     sync.Mutex
	states map[net.Conn]http.ConnState
}

func (c *refreshConnectionStates) update(conn net.Conn, state http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if state == http.StateClosed || state == http.StateHijacked {
		delete(c.states, conn)
		return
	}
	c.states[conn] = state
}

func (c *refreshConnectionStates) count(state http.ConnState) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	for _, current := range c.states {
		if current == state {
			count++
		}
	}
	return count
}

func (c *refreshConnectionStates) open() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.states)
}

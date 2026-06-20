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

package httpc_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// idleCloseRecorder is a RoundTripper that records CloseIdleConnections calls.
type idleCloseRecorder struct {
	base   http.RoundTripper
	closed atomic.Int32
}

func (r *idleCloseRecorder) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.base.RoundTrip(req)
}

func (r *idleCloseRecorder) CloseIdleConnections() { r.closed.Add(1) }

// TestBuildHTTPClient_CloseIdleConnectionsPropagates verifies the *http.Client
// from BuildHTTPClient forwards CloseIdleConnections through every wrapper layer
// (auth/header decoration + telemetry) down to the underlying transport.
func TestBuildHTTPClient_CloseIdleConnectionsPropagates(t *testing.T) {
	rec := &idleCloseRecorder{base: http.DefaultTransport}

	httpClient, err := httpc.NewBuilder().
		SetAuthToken("token"). // forces the decoration wrapper layer in addition to telemetry
		SetHeader("X-Test", "v").
		SetTransport(rec).
		BuildHTTPClient(context.Background())
	require.NoError(t, err)

	httpClient.Current().CloseIdleConnections()
	assert.Equal(t, int32(1), rec.closed.Load(), "CloseIdleConnections should reach the base transport through the wrappers")
}

// connTracker counts server-side connections that are open (not yet closed).
type connTracker struct {
	mu   sync.Mutex
	open map[net.Conn]struct{}
}

func newConnTracker() *connTracker { return &connTracker{open: map[net.Conn]struct{}{}} }

func (c *connTracker) onState(conn net.Conn, state http.ConnState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch state {
	case http.StateClosed, http.StateHijacked:
		delete(c.open, conn)
	default:
		c.open[conn] = struct{}{}
	}
}

func (c *connTracker) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.open)
}

func newConnTrackedServer(t *testing.T) (*httptest.Server, *connTracker) {
	tracker := newConnTracker()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ConnState = tracker.onState
	server.Start()
	t.Cleanup(server.Close)
	return server, tracker
}

// doIdleRequest makes a GET, drains and closes the body so the connection
// returns to the transport's idle pool.
func doIdleRequest(t *testing.T, client *http.Client, url string) {
	t.Helper()
	resp, err := client.Get(url)
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
}

// TestBuildTransport_CloseIdleConnections_RealTransport verifies that calling
// CloseIdleConnections on a client backed by the built (refreshable) transport
// actually closes the live *http.Transport's idle connection.
func TestBuildTransport_CloseIdleConnections_RealTransport(t *testing.T) {
	server, tracker := newConnTrackedServer(t)

	rt, err := httpc.NewBuilder().BuildTransport(context.Background())
	require.NoError(t, err)
	client := &http.Client{Transport: rt}

	doIdleRequest(t, client, server.URL)
	require.Eventually(t, func() bool { return tracker.count() == 1 }, 2*time.Second, 10*time.Millisecond,
		"expected one idle connection after the request")

	client.CloseIdleConnections()
	require.Eventually(t, func() bool { return tracker.count() == 0 }, 2*time.Second, 10*time.Millisecond,
		"idle connection should be closed after CloseIdleConnections")
}

// TestBuildTransport_RefreshClosesRetiredTransport verifies that a config-driven
// transport rebuild closes the retired transport's idle connections.
func TestBuildTransport_RefreshClosesRetiredTransport(t *testing.T) {
	server, tracker := newConnTrackedServer(t)
	ctx := context.Background()

	cfg := httpc.ClientConfig{ServiceName: "test", URIs: []string{server.URL}, MaxIdleConns: new(50)}
	cfgR := refreshable.New(cfg)

	rt, err := httpc.NewBuilder().ApplyConfigRefreshable(ctx, cfgR).BuildTransport(ctx)
	require.NoError(t, err)
	client := &http.Client{Transport: rt}

	// Open an idle connection on the current ("v1") transport.
	doIdleRequest(t, client, server.URL)
	require.Eventually(t, func() bool { return tracker.count() == 1 }, 2*time.Second, 10*time.Millisecond,
		"expected one idle connection after the request")

	// Change a transport param to trigger a rebuild; the retired transport's idle
	// connections should be closed.
	cfg2 := cfg
	cfg2.MaxIdleConns = new(60)
	cfgR.Update(cfg2)

	require.Eventually(t, func() bool { return tracker.count() == 0 }, 2*time.Second, 10*time.Millisecond,
		"retired transport's idle connection should be closed on rebuild")
}

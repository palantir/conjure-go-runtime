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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestRetiredCanceledHTTP2Stream(t *testing.T) {
	for _, retireFirst := range []bool{false, true} {
		for _, activeStream := range []bool{false, true} {
			t.Run(fmt.Sprintf("retireFirst=%t/activeStream=%t", retireFirst, activeStream), func(t *testing.T) {
				finish, closed := make(chan struct{}), make(chan struct{}, 1)
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.(http.Flusher).Flush()
					if r.URL.Path == "/cancel" {
						<-r.Context().Done()
						return
					}
					<-finish
					_, _ = io.WriteString(w, "ok")
				}))
				server.EnableHTTP2 = true
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateClosed {
						closed <- struct{}{}
					}
				}
				server.StartTLS()
				t.Cleanup(server.Close)
				complete := sync.OnceFunc(func() { close(finish) })
				t.Cleanup(complete)
				dialer := &gatedDialer{}
				transport := &managedTransport{transport: newTransport(t.Context(), TransportParams{}, &tls.Config{InsecureSkipVerify: true}, dialer)}
				t.Cleanup(transport.transport.CloseIdleConnections)
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/cancel", nil)
				require.NoError(t, err)
				resp, err := transport.RoundTrip(req)
				require.NoError(t, err)
				var healthy *http.Response
				if activeStream {
					var reused bool
					ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) { reused = info.Reused }})
					req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
					require.NoError(t, err)
					healthy, err = transport.RoundTrip(req)
					require.NoError(t, err)
					require.True(t, reused)
				}
				if retireFirst {
					transport.retire()
				}
				dialer.conn.block.Store(true)
				release := sync.OnceFunc(func() { close(dialer.conn.release) })
				t.Cleanup(release)
				cancel()
				select {
				case <-dialer.conn.blocked:
				case <-time.After(5 * time.Second):
					t.Fatal("stream reset not attempted")
				}
				require.NoError(t, resp.Body.Close())
				require.NoError(t, resp.Body.Close())
				if !retireFirst {
					transport.retire()
				}
				release()
				complete()
				if healthy != nil {
					data, err := io.ReadAll(healthy.Body)
					require.NoError(t, err)
					require.Equal(t, "ok", string(data))
					require.NoError(t, healthy.Body.Close())
				}
				select {
				case <-closed:
				case <-time.After(5 * time.Second):
					t.Fatal("retired canceled stream left its connection open")
				}
			})
		}
	}
}

type gatedConn struct {
	net.Conn
	block            atomic.Bool
	blocked, release chan struct{}
	once             sync.Once
}

func (c *gatedConn) Write(p []byte) (int, error) {
	if c.block.Load() {
		c.once.Do(func() { close(c.blocked) })
		<-c.release
	}
	return c.Conn.Write(p)
}

type gatedDialer struct{ conn *gatedConn }

func (d *gatedDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
	if err != nil {
		return nil, err
	}
	d.conn = &gatedConn{Conn: conn, blocked: make(chan struct{}), release: make(chan struct{})}
	return d.conn, nil
}

func TestTransportReleasesClosedHTTP2Connections(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	transport := &managedTransport{transport: newTransport(t.Context(), TransportParams{}, &tls.Config{InsecureSkipVerify: true}, &net.Dialer{})}
	func() {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, server.URL, nil)
		require.NoError(t, err)
		resp, err := transport.RoundTrip(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}()
	transport.transport.CloseIdleConnections()
	require.Eventually(t, func() bool {
		runtime.GC()
		transport.mu.Lock()
		defer transport.mu.Unlock()
		return len(transport.connections) == 0
	}, 5*time.Second, 10*time.Millisecond)
}

func TestRefreshableTransportTLSRecovery(t *testing.T) {
	for _, failure := range []string{"invalid CA", "file read"} {
		t.Run(failure, func(t *testing.T) {
			params := refreshable.New(TLSParams{})
			validated, _, err := refreshable.MapWithError(t.Context(), params, func(_ context.Context, p TLSParams) (TLSParams, error) {
				if failure == "file read" && len(p.CABytes) > 0 {
					return TLSParams{}, errors.New("file read failed")
				}
				return p, nil
			})
			require.NoError(t, err)
			tlsConfig, err := NewRefreshableTLSConfig(t.Context(), validated)
			require.NoError(t, err)
			transport := NewRefreshableTransport(t.Context(), refreshable.New(TransportParams{}), tlsConfig, &net.Dialer{}).(*RefreshableTransport)
			initial := transport.CurrentTransport()

			params.Update(TLSParams{CABytes: [][]byte{[]byte("invalid CA")}})
			_, err = tlsConfig.Validation()
			require.Error(t, err)
			require.Same(t, initial, transport.CurrentTransport())

			params.Update(TLSParams{})
			_, err = tlsConfig.Validation()
			require.NoError(t, err)
			require.Same(t, initial, transport.CurrentTransport())

			params.Update(TLSParams{InsecureSkipVerify: true})
			_, err = tlsConfig.Validation()
			require.NoError(t, err)
			require.NotSame(t, initial, transport.CurrentTransport())
		})
	}
}

func TestRefreshableTransportBuildsOncePerTLSUpdate(t *testing.T) {
	params := refreshable.New(TransportParams{})
	tlsParams, _, err := refreshable.MapWithError(t.Context(), params, func(_ context.Context, p TransportParams) (TLSParams, error) {
		return TLSParams{InsecureSkipVerify: p.TLSConfigurationParams.InsecureSkipVerify}, nil
	})
	require.NoError(t, err)
	tlsConfig, err := NewRefreshableTLSConfig(t.Context(), tlsParams)
	require.NoError(t, err)
	transport := NewRefreshableTransport(t.Context(), params, tlsConfig, &net.Dialer{}).(*RefreshableTransport)
	builds := 0
	defer transport.states.Subscribe(func(transportState) { builds++ })()
	require.Equal(t, 1, builds)
	next := params.Current()
	next.TLSConfigurationParams.InsecureSkipVerify = true
	params.Update(next)
	require.Equal(t, 2, builds)
	require.True(t, transport.CurrentTransport().TLSClientConfig.InsecureSkipVerify)
	next.MaxIdleConns = 10
	params.Update(next)
	require.Equal(t, 3, builds)
	require.Equal(t, 10, transport.CurrentTransport().MaxIdleConns)
	params.Update(next)
	require.Equal(t, 3, builds)
}

func TestTransportOwnsTLSConfig(t *testing.T) {
	protocols := []string{"h2", "spare capacity"}
	config := &tls.Config{NextProtos: protocols[:1]}
	transport := newTransport(t.Context(), TransportParams{}, config, &net.Dialer{})
	transport.CloseIdleConnections()
	require.NotSame(t, config, transport.TLSClientConfig)
	require.Equal(t, []string{"h2", "spare capacity"}, protocols)
	transport.TLSClientConfig.NextProtos[0] = "changed"
	require.Equal(t, "h2", config.NextProtos[0])
}

func TestRefreshableTLSConfigConcurrentClone(t *testing.T) {
	params := refreshable.New(TLSParams{})
	validatedParams, _, err := refreshable.Validate(t.Context(), params, func(context.Context, TLSParams) error {
		return nil
	})
	require.NoError(t, err)
	config, err := NewRefreshableTLSConfig(t.Context(), validatedParams)
	require.NoError(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		for range 200 {
			config.Unvalidated().config().Clone()
		}
	})
	close(start)
	for i := range 200 {
		params.Update(TLSParams{DynamicCertReload: i%2 == 0})
	}
	wg.Wait()
}

func TestRefreshableTransportRetirement(t *testing.T) {
	for _, protocol := range []int{1, 2} {
		for _, timing := range []string{"before request", "before headers", "body open", "body closed early", "idle"} {
			t.Run(fmt.Sprintf("HTTP%d/%s", protocol, timing), func(t *testing.T) {
				started, headers, body, closed := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
				sendHeaders := sync.OnceFunc(func() { close(headers) })
				sendBody := sync.OnceFunc(func() { close(body) })
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					close(started)
					<-headers
					w.WriteHeader(http.StatusOK)
					w.(http.Flusher).Flush()
					<-body
					_, _ = io.WriteString(w, "ok")
				}))
				server.EnableHTTP2 = protocol == 2
				server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
					if state == http.StateClosed {
						close(closed)
					}
				}
				server.StartTLS()
				t.Cleanup(server.Close)
				t.Cleanup(sendBody)
				t.Cleanup(sendHeaders)
				tlsConfig, err := refreshable.ValidateAuto(t.Context(), refreshable.New(WrapTLSConfig(&tls.Config{InsecureSkipVerify: true})), func(context.Context, *TLSConfig) error { return nil })
				require.NoError(t, err)
				params := refreshable.New(TransportParams{DisableHTTP2: protocol == 1})
				transport := NewRefreshableTransport(t.Context(), params, tlsConfig, &net.Dialer{}).(*RefreshableTransport)
				selected := transport.states.Current()()
				t.Cleanup(selected.transport.CloseIdleConnections)
				refresh := func() {
					next := params.Current()
					next.MaxIdleConns++
					params.Update(next)
					require.NotSame(t, selected.transport, transport.CurrentTransport())
				}
				if timing == "before request" {
					refresh()
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
				require.NoError(t, err)
				var resp *http.Response
				done := make(chan struct{})
				go func() {
					resp, err = selected.RoundTrip(req)
					close(done)
				}()
				select {
				case <-started:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if timing == "before headers" {
					refresh()
				}
				sendHeaders()
				<-done
				require.NoError(t, err)
				require.Equal(t, protocol, resp.ProtoMajor)
				if timing == "body open" || timing == "body closed early" {
					refresh()
				}
				if timing != "body closed early" {
					sendBody()
					data, err := io.ReadAll(resp.Body)
					require.NoError(t, err)
					require.Equal(t, "ok", string(data))
				}
				require.NoError(t, resp.Body.Close())
				sendBody()
				if timing == "idle" {
					refresh()
				}
				select {
				case <-closed:
				case <-ctx.Done():
					t.Fatal("retired transport retained an idle connection")
				}
			})
		}
	}
}

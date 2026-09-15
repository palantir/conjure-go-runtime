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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/require"
)

func TestRefreshableTransportIgnoresInvalidTLSUpdates(t *testing.T) {
	params := refreshable.New(TLSParams{})
	validatedParams, _, err := refreshable.Validate(t.Context(), params, func(context.Context, TLSParams) error {
		return nil
	})
	require.NoError(t, err)
	validatedTLSConfig, err := NewRefreshableTLSConfig(t.Context(), validatedParams)
	require.NoError(t, err)

	transport := NewRefreshableTransport(t.Context(), refreshable.New(TransportParams{}), validatedTLSConfig, &net.Dialer{}).(*RefreshableTransport)
	initial := transport.CurrentTransport()

	params.Update(TLSParams{CABytes: [][]byte{[]byte("invalid CA")}})
	_, err = validatedTLSConfig.Validation()
	require.Error(t, err)
	require.Same(t, initial, transport.CurrentTransport())

	params.Update(TLSParams{InsecureSkipVerify: true})
	_, err = validatedTLSConfig.Validation()
	require.NoError(t, err)
	require.NotSame(t, initial, transport.CurrentTransport())
}

func TestTransportOwnsTLSConfig(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	t.Cleanup(server.Close)
	config := &tls.Config{NextProtos: []string{"http/1.1"}, InsecureSkipVerify: true}
	h2 := newTransport(t.Context(), TransportParams{}, config, &net.Dialer{})
	h1 := newTransport(t.Context(), TransportParams{DisableHTTP2: true}, config, &net.Dialer{})
	for transport, protocol := range map[*http.Transport]int{h2: 2, h1: 1} {
		t.Cleanup(transport.CloseIdleConnections)
		resp, err := (&http.Client{Transport: transport}).Get(server.URL)
		require.NoError(t, err)
		require.Equal(t, protocol, resp.ProtoMajor)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.Equal(t, []string{"http/1.1"}, config.NextProtos)
	require.NotSame(t, config, h2.TLSClientConfig)
	require.NotSame(t, config, h1.TLSClientConfig)
}

func TestRefreshableTLSConfigConcurrentClone(t *testing.T) {
	params := refreshable.New(TLSParams{})
	validatedParams, _, err := refreshable.Validate(t.Context(), params, func(context.Context, TLSParams) error {
		return nil
	})
	require.NoError(t, err)
	config, err := NewRefreshableTLSConfig(t.Context(), validatedParams)
	require.NoError(t, err)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				config.Unvalidated().config().Clone()
			}
		}
	}()
	for i := range 200 {
		params.Update(TLSParams{DynamicCertReload: i%2 == 0})
	}
	close(stop)
	<-done
}

func TestManagedTransportRetirement(t *testing.T) {
	for _, timing := range []string{"before request", "during request"} {
		t.Run(timing, func(t *testing.T) {
			started := make(chan struct{})
			finish := make(chan struct{})
			closed := make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-finish
				_, _ = io.WriteString(w, "ok")
			}))
			server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					close(closed)
				}
			}
			server.Start()
			t.Cleanup(server.Close)
			finishRequest := sync.OnceFunc(func() { close(finish) })
			t.Cleanup(finishRequest)
			state := &managedTransport{transport: &http.Transport{}}
			t.Cleanup(state.transport.CloseIdleConnections)
			if timing == "before request" {
				state.retire()
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			var resp *http.Response
			done := make(chan struct{})
			go func() {
				resp, err = state.RoundTrip(req)
				close(done)
			}()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if timing == "during request" {
				state.retire()
			}
			finishRequest()
			<-done
			require.NoError(t, err)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Equal(t, "ok", string(body))
			select {
			case <-closed:
			case <-ctx.Done():
				t.Fatal("retired transport retained an idle connection")
			}
		})
	}
}

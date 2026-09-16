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
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"runtime"
	"slices"
	"sync"
	"time"
	"weak"

	"github.com/palantir/pkg/refreshable/v2"
	"github.com/palantir/witchcraft-go-logging/wlog/svclog/svc1log"
	"golang.org/x/net/http2"
)

type TransportParams struct {
	MaxIdleConns          int
	MaxIdleConnsPerHost   int
	DisableHTTP2          bool
	DisableKeepAlives     bool
	IdleConnTimeout       time.Duration
	ExpectContinueTimeout time.Duration
	ResponseHeaderTimeout time.Duration
	TLSHandshakeTimeout   time.Duration
	ProxyURL              *url.URL
	ProxyFromEnvironment  bool
	HTTP2ReadIdleTimeout  time.Duration
	HTTP2PingTimeout      time.Duration

	TLSConfigurationParams TLSConfigurationParams
}

type TLSConfigurationParams struct {
	CAFiles            []string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
	DynamicCertReload  bool
}

func NewRefreshableTransport(ctx context.Context, p refreshable.Refreshable[TransportParams], refreshableConfig refreshable.Validated[*TLSConfig], dialer ContextDialer) http.RoundTripper {
	validTLSConfig := refreshable.MapFromValidatedAuto(refreshableConfig, func(t *TLSConfig) *TLSConfig { return t })
	inputs := refreshable.MergeAuto(validTLSConfig, p, func(t *TLSConfig, p TransportParams) transportInputs {
		// TLS changes arrive through validTLSConfig after validation.
		p.TLSConfigurationParams = TLSConfigurationParams{}
		return transportInputs{tls: t, params: p}
	})
	rebuild := false
	states := refreshable.MapAuto(inputs, func(input transportInputs) transportState {
		if !rebuild {
			rebuild = true
		} else {
			svc1log.FromContext(ctx).Debug("Reconstructing HTTP Transport")
		}
		state := &managedTransport{transport: newTransport(ctx, input.params, input.tls.config(), dialer)}
		return func() *managedTransport { return state }
	})
	var previous *managedTransport
	unsubscribe := states.Subscribe(func(currentState transportState) {
		current := currentState()
		if previous != nil && previous != current {
			previous.retire()
		}
		previous = current
	})
	result := &RefreshableTransport{states: states}
	runtime.AddCleanup(result, func(unsubscribe refreshable.UnsubscribeFunc) {
		unsubscribe()
		previous.retire()
	}, unsubscribe)
	return result
}

type transportState func() *managedTransport

type transportInputs struct {
	tls    *TLSConfig
	params TransportParams
}

// RefreshableTransport implements http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
// The refreshable stores a function because its equality checks use reflect.DeepEqual.
// Storing the transport directly would inspect connection-pool state while net/http concurrently mutates it.
type RefreshableTransport struct {
	states refreshable.Refreshable[transportState]
}

func (r *RefreshableTransport) CurrentTransport() *http.Transport {
	return r.states.Current()().transport
}

func (r *RefreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.states.Current()().RoundTrip(req)
}

func (r *RefreshableTransport) CloseIdleConnections() {
	r.CurrentTransport().CloseIdleConnections()
}

type managedTransport struct {
	transport *http.Transport

	mu      sync.Mutex
	retired bool
	// Zero-count connections may still have canceled streams draining when retirement begins.
	connections map[weak.Pointer[tls.Conn]]int
}

func (t *managedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var connections []weak.Pointer[tls.Conn]
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			conn, ok := info.Conn.(*tls.Conn)
			if !ok || conn.ConnectionState().NegotiatedProtocol != http2.NextProtoTLS {
				return
			}
			key := weak.Make(conn)
			t.mu.Lock()
			if t.connections == nil {
				t.connections = make(map[weak.Pointer[tls.Conn]]int)
			}
			if _, exists := t.connections[key]; !exists {
				// Neither the bookkeeping nor its cleanup may keep a closed connection alive.
				runtime.AddCleanup(conn, func(ownerP weak.Pointer[managedTransport]) {
					if owner := ownerP.Value(); owner != nil {
						owner.mu.Lock()
						delete(owner.connections, key)
						owner.mu.Unlock()
					}
				}, weak.Make(t))
			}
			t.connections[key]++
			connections = append(connections, key)
			t.mu.Unlock()
		},
	}))
	release := sync.OnceFunc(func() {
		t.mu.Lock()
		for _, key := range connections {
			if _, exists := t.connections[key]; exists {
				t.connections[key]--
			}
		}
		t.mu.Unlock()
		t.closeRetiredIdleConnections()
	})
	resp, err := t.transport.RoundTrip(req)
	if err == nil && resp.ProtoMajor == 2 && resp.Body != http.NoBody {
		resp.Body = &retiringResponseBody{ReadCloser: resp.Body, release: release}
	} else {
		release()
	}
	return resp, err
}

func (t *managedTransport) closeRetiredIdleConnections() {
	t.mu.Lock()
	if !t.retired {
		t.mu.Unlock()
		return
	}
	t.transport.CloseIdleConnections()
	// Body.Close can return before a canceled HTTP/2 stream finishes teardown.
	// Close only connections whose callers have all released their responses.
	for key, requests := range t.connections {
		if requests == 0 {
			if conn := key.Value(); conn != nil {
				_ = conn.Close()
			}
			delete(t.connections, key)
		}
	}
	t.mu.Unlock()
}

type retiringResponseBody struct {
	io.ReadCloser
	release func()
}

func (b *retiringResponseBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

func (t *managedTransport) retire() {
	t.mu.Lock()
	t.retired = true
	t.mu.Unlock()
	t.closeRetiredIdleConnections()
}

func newTransport(ctx context.Context, p TransportParams, tlsConfig *tls.Config, dialer ContextDialer) *http.Transport {
	var transportProxy func(*http.Request) (*url.URL, error)
	if p.ProxyURL != nil {
		transportProxy = http.ProxyURL(p.ProxyURL)
	} else if p.ProxyFromEnvironment {
		transportProxy = http.ProxyFromEnvironment
	}

	// HTTP/2 setup modifies NextProtos; each transport must own its config.
	tlsConfig = tlsConfig.Clone()
	if tlsConfig != nil {
		tlsConfig.NextProtos = slices.Clone(tlsConfig.NextProtos)
	}
	transport := &http.Transport{
		Proxy:                 transportProxy,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       tlsConfig,
		DisableKeepAlives:     p.DisableKeepAlives,
		ExpectContinueTimeout: p.ExpectContinueTimeout,
		IdleConnTimeout:       p.IdleConnTimeout,
		TLSHandshakeTimeout:   p.TLSHandshakeTimeout,
		ResponseHeaderTimeout: p.ResponseHeaderTimeout,
	}

	if !p.DisableHTTP2 {
		// Attempt to configure net/http HTTP/1 Transport to use HTTP/2.
		http2Transport, err := http2.ConfigureTransports(transport)
		if err != nil {
			// ConfigureTransport's only error as of this writing is the idempotent "protocol https already registered."
			// It should never happen in our usage because this is immediately after creation.
			// In case of something unexpected, log it and move on.
			svc1log.FromContext(ctx).Error("failed to configure transport for http2", svc1log.Stacktrace(err))
		} else {
			// ReadIdleTimeout is the amount of time to wait before running periodic health checks (pings)
			// after not receiving a frame from the HTTP/2 connection.
			// Setting this value will enable the health checks and allows broken idle
			// connections to be pruned more quickly, preventing the client from
			// attempting to re-use connections that will no longer work.
			// ref: https://github.com/golang/go/issues/36026
			http2Transport.ReadIdleTimeout = p.HTTP2ReadIdleTimeout

			// PingTimeout configures the amount of time to wait for a ping response (health check)
			// before closing an HTTP/2 connection. The PingTimeout is only valid if
			// the above ReadIdleTimeout is > 0.
			http2Transport.PingTimeout = p.HTTP2PingTimeout
		}
	}

	return transport
}

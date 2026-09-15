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
	"net/http"
	"net/url"
	"runtime"
	"sync/atomic"
	"time"

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
	HTTPProxyURL          *url.URL
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
	states := refreshable.MergeAuto(validTLSConfig, p, func(t *TLSConfig, p TransportParams) transportState {
		state := &managedTransport{transport: newTransport(ctx, p, t.config(), dialer)}
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
	runtime.AddCleanup(result, unsubscribeRefreshable, unsubscribe)
	return result
}

type transportState func() *managedTransport

// RefreshableTransport implements http.RoundTripper backed by a refreshable *http.Transport.
// The transport and internal dialer are each rebuilt when any of their respective parameters are updated.
// The refreshable stores a function because its equality checks use reflect.DeepEqual.
// Storing the transport directly would inspect connection-pool state while net/http concurrently mutates it.
type RefreshableTransport struct {
	states refreshable.Refreshable[transportState]
}

func unsubscribeRefreshable(unsubscribe refreshable.UnsubscribeFunc) {
	unsubscribe()
}

func (r *RefreshableTransport) CurrentTransport() *http.Transport {
	return r.states.Current()().transport
}

func (r *RefreshableTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return r.states.Current()().RoundTrip(req)
}

type managedTransport struct {
	transport *http.Transport

	retired atomic.Bool
}

func (t *managedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	defer func() {
		// A request that selected this transport before retirement can start
		// afterward, undoing CloseIdleConnections. Close again when it returns.
		if t.retired.Load() {
			t.transport.CloseIdleConnections()
		}
	}()
	return t.transport.RoundTrip(req)
}

func (t *managedTransport) retire() {
	t.retired.Store(true)
	t.transport.CloseIdleConnections()
}

func newTransport(ctx context.Context, p TransportParams, tlsConfig *tls.Config, dialer ContextDialer) *http.Transport {
	svc1log.FromContext(ctx).Debug("Reconstructing HTTP Transport")

	var transportProxy func(*http.Request) (*url.URL, error)
	if p.HTTPProxyURL != nil {
		transportProxy = func(*http.Request) (*url.URL, error) { return p.HTTPProxyURL, nil }
	} else if p.ProxyFromEnvironment {
		transportProxy = http.ProxyFromEnvironment
	}

	// HTTP/2 setup modifies NextProtos; each transport must own its config.
	transport := &http.Transport{
		Proxy:                 transportProxy,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          p.MaxIdleConns,
		MaxIdleConnsPerHost:   p.MaxIdleConnsPerHost,
		TLSClientConfig:       tlsConfig.Clone(),
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

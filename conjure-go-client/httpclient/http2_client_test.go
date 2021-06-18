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

// +build go1.16

package httpclient_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-server/httpserver"
	"github.com/stretchr/testify/require"
)

// TestHTTP2Client_reusesBrokenConnection asserts the behavior of an HTTP/2 client that re-uses
// a broken connection and fails to make requests. The clients ReadIdleTimeout is set to 0, which means
// there will be no HTTP/2 health checks enabled and thus the client will re-use existing (broken) connections.
func TestHTTP2Client_reusesBrokenConnection(t *testing.T) {
	// The second request, which re-uses the broken connection, will cause an error
	// "use of closed network connection" so we should only expect to have 1 dial succeed.
	testProxy(t, 0, 0, 1, true)
}

// TestHTTP2Client_reconnectsOnBrokenConnection asserts the behavior of an HTTP/2 client that has
// the ReadIdleTimeout configured very low which forces the client to re-connect on subsequent requests.
func TestHTTP2Client_reconnectsOnBrokenConnection(t *testing.T) {
	testProxy(t, time.Second, time.Second, 2, false)
}

func testProxy(t *testing.T, readIdleTimeout, pingTimeout time.Duration, expectedDials int, expectErr bool) {
	ctx := context.Background()

	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpserver.WriteJSONResponse(w, map[string]string{
			"proto": r.Proto,
		}, http.StatusOK)
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	u, err := url.Parse(ts.URL)
	require.NoError(t, err)
	proxy := newProxyServer(t, u.Host)
	defer func() {
		require.NoError(t, proxy.ln.Close())
	}()
	stopCh := make(chan struct{})
	go proxy.serve(t, stopCh, false)

	client, err := httpclient.NewClient(
		httpclient.WithBaseURLs([]string{"https://" + proxy.ln.Addr().String()}),
		httpclient.WithHTTP2ReadIdleTimeout(readIdleTimeout),
		httpclient.WithHTTP2PingTimeout(pingTimeout),
		httpclient.WithTLSInsecureSkipVerify(),
		httpclient.WithDisableRestErrors(),
		httpclient.WithMaxRetries(0),
		httpclient.WithHTTPTimeout(time.Second),
	)
	require.NoError(t, err)

	expectedRespBody := map[string]string{
		"proto": "HTTP/2.0",
	}
	var actualResp map[string]string

	// First request should always succeed
	resp, err := client.Get(ctx,
		httpclient.WithJSONResponse(&actualResp),
	)
	require.NoError(t, err)
	require.Equal(t, resp.StatusCode, http.StatusOK)
	require.Equal(t, expectedRespBody, actualResp)

	// close the proxy to simulate a broken TCP connection
	close(stopCh)

	// restart the proxy with the expected error
	stopCh = make(chan struct{})
	go proxy.serve(t, stopCh, expectErr)
	defer close(stopCh)

	<-time.After(time.Second + readIdleTimeout + pingTimeout)

	// Second Request
	resp, err = client.Get(ctx,
		httpclient.WithJSONResponse(&actualResp),
	)
	if expectErr {
		require.Error(t, err)
		require.Nil(t, resp)
	} else {
		require.NoError(t, err)
		require.Equal(t, resp.StatusCode, http.StatusOK)
		require.Equal(t, expectedRespBody, actualResp)
	}
	require.Equal(t, expectedDials, proxy.DialCount())
}

type proxyServer struct {
	ln        net.Listener
	proxyURL  string
	dialCount int32
}

func newProxyServer(t *testing.T, proxyURL string) *proxyServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return &proxyServer{
		proxyURL: proxyURL,
		ln:       ln,
	}
}

func (p *proxyServer) serve(t *testing.T, stopCh chan struct{}, expectErr bool) {
	conn, err := p.ln.Accept()
	if expectErr {
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "use of closed network connection"))
		return
	}
	require.NoError(t, err)
	atomic.AddInt32(&p.dialCount, 1)
	go p.handleConnection(t, conn, stopCh)
}

func (p *proxyServer) handleConnection(t *testing.T, in net.Conn, stopCh chan struct{}) {
	out, err := net.Dial("tcp", p.proxyURL)
	require.NoError(t, err)
	go func() {
		_, _ = io.Copy(out, in)
	}()
	go func() {
		_, _ = io.Copy(in, out)
	}()
	<-stopCh
	require.NoError(t, out.Close())
}

func (p *proxyServer) DialCount() int {
	return int(atomic.LoadInt32(&p.dialCount))
}

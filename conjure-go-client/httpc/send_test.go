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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSend_PerRequestMiddlewareRunsPerAttempt verifies a per-request middleware
// runs on every attempt with the resolved per-attempt URL, not just once.
func TestSend_PerRequestMiddlewareRunsPerAttempt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			// First attempt: close the connection to trigger a retryable error.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	var mwCalls atomic.Int32
	var sawResolvedURL atomic.Bool
	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		mwCalls.Add(1)
		if req.URL.IsAbs() && req.URL.Host != "" {
			sawResolvedURL.Store(true)
		}
		return next.RoundTrip(req)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("per-attempt").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("PerAttempt", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	_, _, err = ep.Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, int32(2), mwCalls.Load(), "per-request middleware should run on each attempt")
	assert.True(t, sawResolvedURL.Load(), "per-request middleware should see the resolved per-attempt URL")
}

// TestSend_PerRequestMiddlewareInsideTelemetry verifies a per-request middleware
// runs inside the intrinsic telemetry stack: a panic in it is caught by the
// telemetry panic-recovery layer and surfaced as an error rather than crashing.
func TestSend_PerRequestMiddlewareInsideTelemetry(t *testing.T) {
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetServiceName("recovery").
		SetTransport(&roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
		}}).
		Build(t.Context())
	require.NoError(t, err)

	mw := httpc.MiddlewareFunc(func(_ *http.Request, _ http.RoundTripper) (*http.Response, error) {
		panic("boom")
	})
	ep := httpc.NewGET[struct{}]("Panic", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	_, _, err = ep.Call().Execute(t.Context(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recovered panic")
}

// TestSend_PerRequestMiddlewareOverridesAuth verifies placement B: per-request
// middleware runs below the client's auth middleware, so it can override the
// Authorization header the client set.
func TestSend_PerRequestMiddlewareOverridesAuth(t *testing.T) {
	var gotAuth string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("auth-override").
		SetAuthToken("client-token").
		Build(t.Context())
	require.NoError(t, err)

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set("Authorization", "Bearer request-token")
		return next.RoundTrip(req)
	})
	ep := httpc.NewGET[struct{}]("AuthOverride", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	_, _, err = ep.Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "Bearer request-token", gotAuth)
}

// TestSend_NilMiddlewareWithPerRequest verifies a Client whose Middleware()
// returns nil still composes per-request middleware and the raw transport
// without panicking.
func TestSend_NilMiddlewareWithPerRequest(t *testing.T) {
	var called bool
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "per-req", r.Header.Get("X-Per-Request"))
		w.WriteHeader(http.StatusNoContent)
	})

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		called = true
		req.Header.Set("X-Per-Request", "per-req")
		return next.RoundTrip(req)
	})

	client := &httpTestClient{server: server} // Middleware() returns nil
	ep := httpc.NewGET[struct{}]("NilMW", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	_, _, err := ep.Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.True(t, called)
}

// TestBuildHTTPClient_CarriesIntrinsicMiddleware verifies the escape-hatch
// *http.Client still carries the intrinsic stack: a configured auth token is
// applied on the wire even though Send's retry/scoring loop is bypassed.
func TestBuildHTTPClient_CarriesIntrinsicMiddleware(t *testing.T) {
	var gotAuth string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})

	refClient, err := httpc.NewBuilder().
		SetServiceName("escape-hatch").
		SetAuthToken("escape-token").
		BuildHTTPClient(context.Background())
	require.NoError(t, err)

	resp, err := refClient.Current().Get(server.URL + "/test")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, "Bearer escape-token", gotAuth)
}

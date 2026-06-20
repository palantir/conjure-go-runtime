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

// A per-request WithHeader resolves above the builder's SetHeader, so the
// per-request value wins (the intrinsic contributor is superseded).
func TestSend_PerRequestHeaderBeatsBuilderHeader(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusNoContent)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("hdr-precedence").
		SetHeader("X-Custom", "builder").
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("Hdr", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("X-Custom", "request")

	_, _, err = ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "request", got)
}

// A declarative Authorization header supersedes the client's auth contributor,
// so its provider never runs — even when that provider would error.
func TestSend_AuthorizationHeaderSuppressesErroringProvider(t *testing.T) {
	var got string
	var providerCalled atomic.Bool
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("Authorization")
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	}}

	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetServiceName("auth-suppress").
		SetAuthTokenProvider(func(context.Context) (string, error) {
			providerCalled.Store(true)
			return "", assert.AnError
		}).
		SetTransport(transport).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("Auth", "/auth").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("Authorization", "Bearer explicit")

	_, _, err = ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "Bearer explicit", got)
	assert.False(t, providerCalled.Load(), "auth provider must not run when Authorization is set explicitly")
}

// Query contributors resolve onto each attempt's freshly cloned request, so a
// retry neither drops nor duplicates an added query parameter.
func TestSend_QueryResolvesPerAttempt(t *testing.T) {
	var attempts atomic.Int32
	var gotQuery []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// First attempt: close the connection to force a retryable error.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		gotQuery = r.URL.Query()["q"]
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("query-per-attempt").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("Q", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedQuery("q", "v")

	_, _, err = ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, []string{"v"}, gotQuery, "query must be resolved once per attempt, not duplicated on retry")
}

// A Client whose Middleware() is nil and that does not contribute intrinsic
// values still has its per-request header contributors decorated onto the request.
func TestSend_NilMiddlewareClientDecoratesHeaders(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusNoContent)
	})

	client := &httpTestClient{server: server} // Middleware() == nil, not an intrinsicValuer
	ep := httpc.NewGET[struct{}]("Hdr", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("X-Custom", "v")

	_, _, err := ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "v", got)
}

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
// limitations under the License

package httpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMiddlewareChain_Order verifies that endpoint middleware executes in the correct order.
// Each successive WithMiddleware wraps the previous, so the last-added middleware
// is outermost (executes first on the way in, last on the way out).
func TestMiddlewareChain_Order(t *testing.T) {
	var order []string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw1 := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "mw1-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "mw1-after")
		return resp, err
	})
	mw2 := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "mw2-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "mw2-after")
		return resp, err
	})
	mw3 := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "mw3-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "mw3-after")
		return resp, err
	})

	// Each WithMiddleware wraps the previous, so mw3 is outermost.
	ep := httpc.NewGET[struct{}]("Order", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw1).
		WithMiddleware(mw2).
		WithMiddleware(mw3)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(t.Context(), client)
	require.NoError(t, err)

	// Last added (mw3) wraps mw2 wraps mw1 wraps client.
	assert.Equal(t, []string{
		"mw3-before", "mw2-before", "mw1-before",
		"mw1-after", "mw2-after", "mw3-after",
	}, order)
}

// TestMiddlewareChain_NilSkipped verifies that nil middlewares are silently skipped.
func TestMiddlewareChain_NilSkipped(t *testing.T) {
	var called bool
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		called = true
		return next.RoundTrip(req)
	})

	// Mix nil and non-nil middleware.
	ep := httpc.NewGET[struct{}]("NilSkip", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(nil).
		WithMiddleware(mw).
		WithMiddleware(nil)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.True(t, called)
}

// TestMiddlewareChain_EmptyPassthrough verifies that an empty middleware chain
// delegates directly to the base transport.
func TestMiddlewareChain_EmptyPassthrough(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"passthrough","value":0}`))
	})

	// No middleware at all.
	ep := httpc.NewGET[testPayload]("Empty", "/test").
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "passthrough", Value: 0}, result)
}

// TestMiddlewareChain_BuiltClient tests the middleware chain through a real built client
// to ensure the iterative chain in doOnce works correctly.
func TestMiddlewareChain_BuiltClient(t *testing.T) {
	var middlewareCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "chain-value", r.Header.Get("X-Chain"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"chain","value":1}`))
	}))
	t.Cleanup(server.Close)

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		middlewareCalled = true
		req.Header.Set("X-Chain", "chain-value")
		return next.RoundTrip(req)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("test-service").
		AddMiddleware(mw).
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testPayload]("ChainTest", "/test").
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Execute(t.Context(), client)
	require.NoError(t, err)
	assert.True(t, middlewareCalled)
	assert.Equal(t, testPayload{Name: "chain", Value: 1}, result)
}

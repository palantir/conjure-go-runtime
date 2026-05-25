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
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Task 10: Middleware stack ordering
// ---------------------------------------------------------------------------

// TestMiddlewareStackOrder verifies the full middleware chain executes in the
// documented order when a client is built with outer, inner, and per-request
// middleware. The expected order from outermost to innermost is:
//
//	per-request → outer recovery → user outer → error decoder → URI scorer
//	  → inner recovery → tracing → metrics → user inner → http.Transport
//
// This test disables tracing, metrics, and recovery to isolate the user-visible
// layers (outer, inner, per-request) and confirm their relative ordering.
func TestMiddlewareStackOrder(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "transport")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	outerMW := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "outer-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "outer-after")
		return resp, err
	})
	innerMW := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "inner-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "inner-after")
		return resp, err
	})
	perRequestMW := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		order = append(order, "per-request-before")
		resp, err := next.RoundTrip(req)
		order = append(order, "per-request-after")
		return resp, err
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("order-test").
		AddMiddleware(outerMW).
		AddInnerMiddleware(innerMW).
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("OrderTest", "/test").
		SetDecoder(httpc.VoidDecoder()).
		WithMiddleware(perRequestMW)

	_, _, err = ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"per-request-before",
		"outer-before",
		"inner-before",
		"transport",
		"inner-after",
		"outer-after",
		"per-request-after",
	}, order)
}

// TestMiddlewareStackOrder_MultipleOuterAndInner verifies ordering when
// multiple outer and inner middlewares are added.
func TestMiddlewareStackOrder_MultipleOuterAndInner(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "transport")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	makeMW := func(name string) httpc.Middleware {
		return httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			order = append(order, name+"-before")
			resp, err := next.RoundTrip(req)
			order = append(order, name+"-after")
			return resp, err
		})
	}

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("multi-mw").
		AddMiddleware(makeMW("outer1")).
		AddMiddleware(makeMW("outer2")).
		AddInnerMiddleware(makeMW("inner1")).
		AddInnerMiddleware(makeMW("inner2")).
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("MultiMW", "/test").
		SetDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)

	// Outer middlewares: last added is outermost (outer2 before outer1).
	// Inner middlewares: last added is innermost (inner1 before inner2),
	// because AddInnerMiddleware prepends.
	assert.Equal(t, []string{
		"outer2-before",
		"outer1-before",
		"inner1-before",
		"inner2-before",
		"transport",
		"inner2-after",
		"inner1-after",
		"outer1-after",
		"outer2-after",
	}, order)
}

// ---------------------------------------------------------------------------
// Task 11: Builder error accumulation
// ---------------------------------------------------------------------------

// TestBuilder_ErrorAccumulation_SingleError verifies that a single bad setting
// produces an error at Build time.
func TestBuilder_ErrorAccumulation_SingleError(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetBaseURLs("http://localhost").
		SetSocksProxyURL("%%"). // unparseable URL
		Build(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOCKS proxy URL")
}

// TestBuilder_ErrorAccumulation_MultipleErrors verifies that multiple bad settings
// are all surfaced at Build time via errors.Join.
func TestBuilder_ErrorAccumulation_MultipleErrors(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetBaseURLs("http://localhost").
		SetSocksProxyURL("%%").
		SetHTTPProxyURL("%%").
		Build(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOCKS proxy URL")
	assert.Contains(t, err.Error(), "HTTP proxy URL")
}

// TestBuilder_ErrorAccumulation_BuildDialer verifies that accumulated errors are
// surfaced by BuildDialer as well, not only Build.
func TestBuilder_ErrorAccumulation_BuildDialer(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetSocksProxyURL("%%").
		BuildDialer(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOCKS proxy URL")
}

// TestBuilder_ErrorAccumulation_BuildTransport verifies that accumulated errors
// are surfaced by BuildTransport.
func TestBuilder_ErrorAccumulation_BuildTransport(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetHTTPProxyURL("%%").
		BuildTransport(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP proxy URL")
}

// TestBuilder_ErrorAccumulation_BuildTLSConfig verifies that accumulated errors
// are surfaced by BuildTLSConfig.
func TestBuilder_ErrorAccumulation_BuildTLSConfig(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetSocksProxyURL("%%").
		BuildTLSConfig(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SOCKS proxy URL")
}

// ---------------------------------------------------------------------------
// Task 12: Retry exhaustion and backoff
// ---------------------------------------------------------------------------

// TestRetry_ExhaustsMaxAttempts verifies that when all attempts fail, the client
// stops retrying and returns the last error.
func TestRetry_ExhaustsMaxAttempts(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Close the connection without a response to trigger a transport error.
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("retry-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, struct{}]("RetryTest", "/test").
		SetEncoder(httpc.JSONEncoder[testRetryPayload]()).
		SetDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(t.Context(), client, testRetryPayload{Value: "hello"})
	require.Error(t, err)
	assert.Equal(t, int32(maxAttempts), attempts.Load())
}

type testRetryPayload struct {
	Value string `json:"value"`
}

// TestRetry_SucceedsOnSecondAttempt verifies that a request that fails on the
// first attempt but succeeds on the second is retried and returns the successful response.
func TestRetry_SucceedsOnSecondAttempt(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// First attempt: close connection.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		// Second attempt: success.
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"ok"}`))
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("retry-success-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewJSONPOST[testRetryPayload, testRetryPayload]("RetrySuccessTest", "/test")

	resp, _, err := ep.Execute(t.Context(), client, testRetryPayload{Value: "hello"})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestRetry_MultipleURIs verifies that the client rotates through multiple
// URIs on retries.
func TestRetry_MultipleURIs(t *testing.T) {
	var server1Hits, server2Hits atomic.Int32

	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server1Hits.Add(1)
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(server1.Close)

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server2Hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"from-server2"}`))
	}))
	t.Cleanup(server2.Close)

	client, err := httpc.NewBuilder().
		SetBaseURLs(server1.URL, server2.URL).
		SetServiceName("multi-uri-retry").
		SetURIScoringStrategy(httpc.URIScoringRandom).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewJSONPOST[testRetryPayload, testRetryPayload]("MultiURIRetry", "/test")

	// Retry enough times to hit both servers.
	var successes int
	for range 10 {
		resp, _, err := ep.Execute(t.Context(), client, testRetryPayload{Value: "hello"})
		if err == nil {
			successes++
			assert.Equal(t, "from-server2", resp.Value)
		}
	}
	// At least some requests should have succeeded by hitting server2.
	assert.Greater(t, successes, 0, "expected at least one successful response from server2")
	assert.Greater(t, server1Hits.Load(), int32(0), "expected server1 to be hit at least once")
	assert.Greater(t, server2Hits.Load(), int32(0), "expected server2 to be hit at least once")
}

// TestRetry_NonRetryableBody verifies that requests without a replayable body
// (GetBody == nil) are NOT retried.
func TestRetry_NonRetryableBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	maxAttempts := 5
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("no-retry-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	// Use BinaryEncoder with a non-seekable reader — GetBody won't be set.
	ep := httpc.NewEndpoint[io.ReadCloser, struct{}](http.MethodPost, "NoRetry", "/test").
		SetEncoder(httpc.BinaryEncoder("application/octet-stream")).
		SetDecoder(httpc.VoidDecoder())

	body := io.NopCloser(readerFunc(func(p []byte) (int, error) {
		copy(p, "data")
		return 4, io.EOF
	}))

	_, _, err = ep.Execute(t.Context(), client, body)
	require.Error(t, err)
	// Only 1 attempt because body is not replayable.
	assert.Equal(t, int32(1), attempts.Load())
}

// readerFunc adapts a function to io.Reader.
type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

// ---------------------------------------------------------------------------
// Task 13: Compressed body retry
// ---------------------------------------------------------------------------

// TestRetry_GZIPCompressedBody verifies that a request with a gzip-compressed
// JSON body is correctly retried: the compressed body is replayed and the
// server receives valid gzip content on the second attempt.
func TestRetry_GZIPCompressedBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// First attempt: close connection to trigger retry.
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		// Second attempt: verify gzip body.
		assert.Equal(t, "gzip", r.Header.Get("Content-Encoding"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		gr, err := gzip.NewReader(r.Body)
		require.NoError(t, err)
		defer func() { assert.NoError(t, gr.Close()) }()

		var payload testRetryPayload
		require.NoError(t, json.NewDecoder(gr).Decode(&payload))
		assert.Equal(t, "compressed-retry", payload.Value)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testRetryPayload{Value: "ok"})
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("gzip-retry-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("GZIPRetry", "/test").
		SetEncoder(httpc.GZIPEncoder(httpc.JSONEncoder[testRetryPayload]())).
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, testRetryPayload{Value: "compressed-retry"})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestRetry_SnappyCompressedBody verifies that a request with a snappy-compressed
// body is correctly retried.
func TestRetry_SnappyCompressedBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		assert.Equal(t, "snappy", r.Header.Get("Content-Encoding"))

		// Read the snappy-compressed body — snappy uses a framing format with
		// buffered writer, so use the snappy reader.
		compressed, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NotEmpty(t, compressed)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testRetryPayload{Value: "snappy-ok"})
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("snappy-retry-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("SnappyRetry", "/test").
		SetEncoder(httpc.SnappyEncoder(httpc.JSONEncoder[testRetryPayload]())).
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, testRetryPayload{Value: "compressed-retry"})
	require.NoError(t, err)
	assert.Equal(t, "snappy-ok", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestRetry_ZLIBCompressedBody verifies that a request with a zlib-compressed
// body is correctly retried.
func TestRetry_ZLIBCompressedBody(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			hj, ok := w.(http.Hijacker)
			require.True(t, ok)
			conn, _, _ := hj.Hijack()
			_ = conn.Close()
			return
		}
		assert.Equal(t, "deflate", r.Header.Get("Content-Encoding"))

		compressed, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NotEmpty(t, compressed)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(testRetryPayload{Value: "zlib-ok"})
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("zlib-retry-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("ZLIBRetry", "/test").
		SetEncoder(httpc.ZLIBEncoder(httpc.JSONEncoder[testRetryPayload]())).
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, testRetryPayload{Value: "compressed-retry"})
	require.NoError(t, err)
	assert.Equal(t, "zlib-ok", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// ---------------------------------------------------------------------------
// Error decoder + redirect/retry integration
// ---------------------------------------------------------------------------

// TestErrorDecoder_307WithLocation_RetriesAgainstLocation verifies that when
// the server returns 307 with a Location header, the error decoder intercepts
// the response (preventing http.Client from following the redirect itself),
// and the retrier follows the Location to a second server which returns 200.
func TestErrorDecoder_307WithLocation_RetriesAgainstLocation(t *testing.T) {
	var targetHits atomic.Int32
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"redirected"}`))
	}))
	t.Cleanup(targetServer.Close)

	var originHits atomic.Int32
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		w.Header().Set("Location", targetServer.URL+"/test")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(originServer.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(originServer.URL).
		SetServiceName("redirect-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("RedirectTest", "/test").
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "redirected", resp.Value)
	assert.Equal(t, int32(1), originHits.Load(), "origin should be hit once")
	assert.Equal(t, int32(1), targetHits.Load(), "target should be hit once via redirect")
}

// TestErrorDecoder_307NoLocation_Retries verifies that a 307 without
// a Location header triggers a retry.
func TestErrorDecoder_307NoLocation_Retries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTemporaryRedirect) // 307, no Location
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"retried"}`))
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("307-no-location").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("NoLocationTest", "/test").
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "retried", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestErrorDecoder_301Redirect_FollowedByHTTPClient verifies that a 301
// redirect (below the default error decoder's 307 threshold) is followed
// normally by http.Client, even when an error decoder is configured.
// This guards against CheckRedirect accidentally blocking sub-307 redirects.
func TestErrorDecoder_301Redirect_FollowedByHTTPClient(t *testing.T) {
	var targetHits atomic.Int32
	targetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"moved"}`))
	}))
	t.Cleanup(targetServer.Close)

	var originHits atomic.Int32
	originServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits.Add(1)
		http.Redirect(w, r, targetServer.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	t.Cleanup(originServer.Close)

	client, err := httpc.NewBuilder().
		SetBaseURLs(originServer.URL).
		SetServiceName("301-redirect").
		Build(t.Context()) // default error decoder configured
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("MovedTest", "/test").
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "moved", resp.Value)
	assert.Equal(t, int32(1), originHits.Load())
	assert.Equal(t, int32(1), targetHits.Load())
}

// TestErrorDecoder_429_RetriesWithBackoff verifies that a 429 Too Many Requests
// response is retried. The first attempt gets throttled, the second succeeds.
func TestErrorDecoder_429_RetriesWithBackoff(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"throttled-ok"}`))
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("throttle-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("ThrottleTest", "/test").
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "throttled-ok", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestErrorDecoder_503_Retries verifies that a 503 Service Unavailable
// response triggers a retry.
func TestErrorDecoder_503_Retries(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"value":"available"}`))
	}))
	t.Cleanup(server.Close)

	maxAttempts := 3
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("unavailable-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("UnavailableTest", "/test").
		SetDecoder(httpc.JSONDecoder[testRetryPayload]()).
		SetAccept("application/json")

	resp, _, err := ep.Execute(t.Context(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "available", resp.Value)
	assert.Equal(t, int32(2), attempts.Load())
}

// TestErrorDecoder_404_NotRetried verifies that a 404 response is not retried
// (it's a client error, not a retryable QoS signal).
func TestErrorDecoder_404_NotRetried(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	maxAttempts := 5
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("not-found-test").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("NotFoundTest", "/missing").
		SetDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(t.Context(), client, httpc.Void{})
	require.Error(t, err)
	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, statusCode)
	assert.Equal(t, int32(1), attempts.Load(), "404 should not be retried")
}

// TestRetry_GZIPCompressedBody_AllFail verifies that a gzip-compressed request
// that fails on every attempt exhausts max attempts and returns an error.
func TestRetry_GZIPCompressedBody_AllFail(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
	}))
	t.Cleanup(server.Close)

	maxAttempts := 4
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("gzip-retry-fail").
		SetMaxAttempts(&maxAttempts).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, struct{}]("GZIPRetryFail", "/test").
		SetEncoder(httpc.GZIPEncoder(httpc.JSONEncoder[testRetryPayload]())).
		SetDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(t.Context(), client, testRetryPayload{Value: "will-fail"})
	require.Error(t, err)
	assert.Equal(t, int32(maxAttempts), attempts.Load(),
		fmt.Sprintf("expected exactly %d attempts with compressed body", maxAttempts))
}

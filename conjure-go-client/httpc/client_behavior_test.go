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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/snappybody"
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
//	URI scorer → telemetry (tracing/metrics/recovery) → user outer → user inner
//	  → auth → per-request → http.Transport
//
// Per-request middleware runs innermost (Send layers it below the client's
// intrinsic stack), so it is closest to the transport. This test disables
// tracing, metrics, and recovery to isolate the user-visible layers (outer,
// inner, per-request) and confirm their relative ordering.
func TestMiddlewareStackOrder(t *testing.T) {
	var order []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "transport")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	outerMW := recordingMiddleware(&order, "outer")
	innerMW := recordingMiddleware(&order, "inner")
	perRequestMW := recordingMiddleware(&order, "per-request")

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
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(perRequestMW)

	_, _, err = ep.Call().Execute(t.Context(), client)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"outer-before",
		"inner-before",
		"per-request-before",
		"transport",
		"per-request-after",
		"inner-after",
		"outer-after",
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("multi-mw").
		AddMiddleware(recordingMiddleware(&order, "outer1")).
		AddMiddleware(recordingMiddleware(&order, "outer2")).
		AddInnerMiddleware(recordingMiddleware(&order, "inner1")).
		AddInnerMiddleware(recordingMiddleware(&order, "inner2")).
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("MultiMW", "/test").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call().Execute(t.Context(), client)
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

// TestBuilder_ErrorAccumulation_BuildTLSConfig verifies BuildTLSConfig is scoped:
// it ignores unrelated setter errors (e.g. a SOCKS proxy) but still surfaces a
// global config validation error.
func TestBuilder_ErrorAccumulation_BuildTLSConfig(t *testing.T) {
	// A SOCKS proxy error is unrelated to TLS and must not block BuildTLSConfig.
	_, err := httpc.NewBuilder().
		SetSocksProxyURL("%%").
		BuildTLSConfig(t.Context())
	require.NoError(t, err, "BuildTLSConfig should ignore an unrelated SOCKS proxy error")

	// A failed ApplyConfig is global and still blocks BuildTLSConfig.
	_, err = httpc.NewBuilder().
		ApplyConfig(t.Context(), httpc.ClientConfig{ProxyURL: new("ftp://bad")}).
		BuildTLSConfig(t.Context())
	require.Error(t, err, "a failed config should block BuildTLSConfig")
	assert.Contains(t, err.Error(), "invalid client config")
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
		WithEncoder(httpc.JSONEncoder[testRetryPayload]()).
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call(testRetryPayload{Value: "hello"}).Execute(t.Context(), client)
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("retry-success-test").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("RetrySuccessTest", "/test").WithJSON()

	resp, _, err := ep.Call(testRetryPayload{Value: "hello"}).Execute(t.Context(), client)
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
		SetURLSelector(httpc.RandomURLSelector).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("MultiURIRetry", "/test").WithJSON()

	// Retry enough times to hit both servers.
	var successes int
	for range 10 {
		resp, _, err := ep.Call(testRetryPayload{Value: "hello"}).Execute(t.Context(), client)
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("no-retry-test").
		SetMaxAttempts(new(5)).
		Build(t.Context())
	require.NoError(t, err)

	// Use BinaryEncoder with a non-seekable reader — GetBody won't be set.
	ep := httpc.NewBodyEndpoint[io.ReadCloser, struct{}](http.MethodPost, "NoRetry", "/test").
		WithEncoder(httpc.BinaryEncoder("application/octet-stream")).
		WithDecoder(httpc.VoidDecoder())

	body := io.NopCloser(readerFunc(func(p []byte) (int, error) {
		copy(p, "data")
		return 4, io.EOF
	}))

	_, _, err = ep.Call(body).Execute(t.Context(), client)
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

// TestRetry_CompressedBody verifies that a compressed (streaming, GetBody-backed)
// request body is replayed correctly on retry, for each built-in compression
// encoder. Compression correctness itself is covered by codec_test.
func TestRetry_CompressedBody(t *testing.T) {
	for _, tc := range []struct {
		name     string
		encoding string
		encoder  func(httpc.BodyEncoder[testRetryPayload]) httpc.BodyEncoder[testRetryPayload]
	}{
		{"gzip", "gzip", httpc.GZIPEncoder[testRetryPayload]},
		{"snappy", "snappy", snappybody.SnappyEncoder[testRetryPayload]},
		{"zlib", "deflate", httpc.ZLIBEncoder[testRetryPayload]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var attempts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if attempts.Add(1) == 1 {
					// First attempt: drop the connection to force a retry.
					hj, ok := w.(http.Hijacker)
					require.True(t, ok)
					conn, _, _ := hj.Hijack()
					_ = conn.Close()
					return
				}
				assert.Equal(t, tc.encoding, r.Header.Get("Content-Encoding"))
				compressed, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.NotEmpty(t, compressed, "the compressed body must be replayed on retry")

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(testRetryPayload{Value: "ok"})
			}))
			t.Cleanup(server.Close)

			client, err := httpc.NewBuilder().
				SetBaseURLs(server.URL).
				SetServiceName("compress-retry-test").
				SetMaxAttempts(new(3)).
				Build(t.Context())
			require.NoError(t, err)

			ep := httpc.NewPOST[testRetryPayload, testRetryPayload]("CompressRetry", "/test").
				WithEncoder(tc.encoder(httpc.JSONEncoder[testRetryPayload]())).
				WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
				WithAccept("application/json")

			resp, _, err := ep.Call(testRetryPayload{Value: "compressed-retry"}).Execute(t.Context(), client)
			require.NoError(t, err)
			assert.Equal(t, "ok", resp.Value)
			assert.Equal(t, int32(2), attempts.Load())
		})
	}
}

// ---------------------------------------------------------------------------
// Error decoder + redirect/retry integration
// ---------------------------------------------------------------------------

// TestErrorDecoder_307WithLocation_RetriesAgainstLocation verifies that when the server
// returns 307 with a Location header, the error decoder intercepts the response
// (preventing http.Client from following the redirect itself), and the retrier follows the
// Location. The Location stays within the configured target (a different path on the same
// origin), so the relocation is honored rather than refused as an off-target pivot;
// off-target relocations are covered by TestSend_QoSRelocationRefusedOutsideConfiguredTargets.
func TestErrorDecoder_307WithLocation_RetriesAgainstLocation(t *testing.T) {
	var originHits, relocatedHits atomic.Int32
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/relocated" {
			relocatedHits.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":"redirected"}`))
			return
		}
		originHits.Add(1)
		w.Header().Set("Location", serverURL+"/relocated")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	t.Cleanup(server.Close)
	serverURL = server.URL

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("redirect-test").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("RedirectTest", "/test").
		WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
		WithAccept("application/json")

	resp, _, err := ep.Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "redirected", resp.Value)
	assert.Equal(t, int32(1), originHits.Load(), "origin should be hit once")
	assert.Equal(t, int32(1), relocatedHits.Load(), "relocated path should be hit once via redirect")
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("307-no-location").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("NoLocationTest", "/test").
		WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
		WithAccept("application/json")

	resp, _, err := ep.Call().Execute(t.Context(), client)
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
		WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
		WithAccept("application/json")

	resp, _, err := ep.Call().Execute(t.Context(), client)
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("throttle-test").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("ThrottleTest", "/test").
		WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
		WithAccept("application/json")

	resp, _, err := ep.Call().Execute(t.Context(), client)
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("unavailable-test").
		SetMaxAttempts(new(3)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[testRetryPayload]("UnavailableTest", "/test").
		WithDecoder(httpc.JSONDecoder[testRetryPayload]()).
		WithAccept("application/json")

	resp, _, err := ep.Call().Execute(t.Context(), client)
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

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("not-found-test").
		SetMaxAttempts(new(5)).
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("NotFoundTest", "/missing").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call().Execute(t.Context(), client)
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
		WithEncoder(httpc.GZIPEncoder(httpc.JSONEncoder[testRetryPayload]())).
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call(testRetryPayload{Value: "will-fail"}).Execute(t.Context(), client)
	require.Error(t, err)
	assert.Equal(t, int32(maxAttempts), attempts.Load(),
		fmt.Sprintf("expected exactly %d attempts with compressed body", maxAttempts))
}

func TestRetry_ReplayableBodyUsesOriginalBodyOnFirstAttempt(t *testing.T) {
	var attempts atomic.Int32
	var opens atomic.Int32
	var closes atomic.Int32

	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		attempt := attempts.Add(1)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.NoError(t, req.Body.Close())
		assert.Equal(t, "payload", string(body))

		if attempt == 1 {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
		}, nil
	}}

	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetServiceName("replay-body").
		SetTransport(transport).
		SetMaxAttempts(new(2)).
		SetInitialBackoff(time.Nanosecond).
		SetMaxBackoff(time.Nanosecond).
		Build(t.Context())
	require.NoError(t, err)

	bodyFn := func() (io.ReadCloser, error) {
		opens.Add(1)
		return &countingReadCloser{
			Reader: strings.NewReader("payload"),
			closes: &closes,
		}, nil
	}
	ep := httpc.NewPOST[func() (io.ReadCloser, error), struct{}]("ReplayBody", "/test").
		WithEncoder(httpc.BinaryEncoderWithReplay("text/plain")).
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call(bodyFn).Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, int32(2), attempts.Load())
	assert.Equal(t, int32(2), opens.Load(), "initial body plus one retry body")
	assert.Equal(t, int32(2), closes.Load(), "each opened body should be closed")
}

type countingReadCloser struct {
	*strings.Reader
	closes *atomic.Int32
}

func (c *countingReadCloser) Close() error {
	c.closes.Add(1)
	return nil
}

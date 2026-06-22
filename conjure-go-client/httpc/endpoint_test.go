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
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEndpointExecute_JSONRoundTrip(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/items", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"test","value":1}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"response","value":2}`))
	})

	ep := httpc.NewBodyEndpoint[testPayload, testPayload](http.MethodPost, "CreateItem", "/api/v1/items").
		WithEncoder(httpc.JSONEncoder[testPayload]()).
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Call(testPayload{Name: "test", Value: 1}).Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "response", Value: 2}, result)
}

func TestEndpointExecute_Headers(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "val1", r.Header.Get("X-Custom"))
		assert.Equal(t, "val2", r.Header.Get("X-Other"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedHeader("X-Custom", "val1").
		WithAddedHeader("X-Other", "val2")

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_QueryParams(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Search", "/search").
		WithDecoder(httpc.VoidDecoder()).
		WithAddedQuery("foo", "bar").
		WithAddedQuery("page", "2")

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_BasicAuth(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "myuser", user)
		assert.Equal(t, "mypass", pass)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").
		WithDecoder(httpc.VoidDecoder()).
		WithAuthorization(httpc.BasicCredentials("myuser", "mypass"))

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_AuthorizerOverridesClientAuth(t *testing.T) {
	var gotAuth string
	var providerCalled bool
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetAuth(httpc.BearerTokenProvider(func(context.Context) (string, error) {
			providerCalled = true
			return "", assert.AnError
		})).
		SetTransport(transport).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").
		WithDecoder(httpc.VoidDecoder()).
		WithAuthorization(httpc.BasicCredentials("request-user", "request-pass"))

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "Basic cmVxdWVzdC11c2VyOnJlcXVlc3QtcGFzcw==", gotAuth)
	assert.False(t, providerCalled)
}

// WithTimeout is per-attempt: Send applies it to the call-scoped *http.Client
// for every attempt, regardless of the Client implementation.
func TestEndpointExecute_Timeout(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})

	client, err := httpc.NewBuilder().
		SetServiceName("timeout-test").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Slow", "/slow").
		WithDecoder(httpc.VoidDecoder()).
		WithTimeout(50 * time.Millisecond)

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
}

func TestEndpointExecute_ZeroTimeoutDoesNotCancelContext(t *testing.T) {
	client := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		require.NoError(t, req.Context().Err())
		return emptyResponse(req), nil
	}}

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithTimeout(0)

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_ErrorDecoder(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(&testErrorDecoder{})

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test error: Forbidden")
}

// TestEndpointExecute_ErrorDecoderFallsBackToDefault verifies that when
// neither the endpoint nor an Overrides supplies an ErrorDecoder, Execute
// falls back to DefaultErrorDecoder().
func TestEndpointExecute_ErrorDecoderFallsBackToDefault(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder())

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	code, ok := httpc.StatusCodeFromError(err)
	require.True(t, ok)
	assert.Equal(t, http.StatusForbidden, code)
}

// TestEndpointExecute_NoErrorDecoderBypassesDefault verifies that explicitly
// installing NoErrorDecoder() opts out of error decoding entirely.
func TestEndpointExecute_NoErrorDecoderBypassesDefault(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Err", "/err").
		WithDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(httpc.NoErrorDecoder())

	_, httpResp, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	require.NotNil(t, httpResp)
	assert.Equal(t, http.StatusForbidden, httpResp.StatusCode)
}

type testErrorDecoder struct{}

func (d *testErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= 400
}

func (d *testErrorDecoder) DecodeError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return &testError{statusCode: resp.StatusCode, body: string(body)}
}

type testError struct {
	statusCode int
	body       string
}

func (e *testError) Error() string {
	return "test error: " + http.StatusText(e.statusCode)
}

func TestClientDo_PreservesEscapedPathSegments(t *testing.T) {
	var gotEscapedPath string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		gotEscapedPath = req.URL.EscapedPath()
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com/base%2Froot").
		SetTransport(transport).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("GetItem", "/items/{itemId}").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Call().WithPathParam("itemId", "a/b").Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "/base%2Froot/items/a%2Fb", gotEscapedPath)
}

func TestEndpointExecute_Middleware(t *testing.T) {
	var middlewareCalled bool
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "middleware-value", r.Header.Get("X-Middleware"))
		w.WriteHeader(http.StatusNoContent)
	})

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		middlewareCalled = true
		req.Header.Set("X-Middleware", "middleware-value")
		return next.RoundTrip(req)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "MW", "/mw").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, middlewareCalled)
}

// TestEndpointExecute_PerRequestMiddlewareInsideTelemetry pins that a per-request
// middleware runs inside telemetry: it observes the For-User-Agent header that
// the telemetry layer injects, which proves telemetry ran first — so the
// middleware is traced/metered and its own request changes are not overwritten.
func TestEndpointExecute_PerRequestMiddlewareInsideTelemetry(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	client, err := httpc.NewBuilder().SetServiceName("svc").SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)

	var seenForUserAgent string
	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		seenForUserAgent = req.Header.Get("For-User-Agent")
		return next.RoundTrip(req)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "T", "/t").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	ctx := httpc.ContextWithForUserAgent(context.Background(), "test-ua")
	_, _, err = ep.Call().Execute(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, "test-ua", seenForUserAgent,
		"per-request middleware should run inside telemetry, after For-User-Agent injection")
}

func TestEndpointExecute_Void(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewNoBodyEndpoint[httpc.Void](http.MethodDelete, "DeleteItem", "/item/1").
		WithDecoder(httpc.VoidDecoder())

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_RPCMethodName(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	var capturedCtx context.Context
	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		capturedCtx = req.Context()
		return next.RoundTrip(req)
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "MyRPCMethod", "/rpc").
		WithDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)

	name, ok := httpc.RPCMethodName(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "MyRPCMethod", name)
}

func TestEndpointExecute_ForUserAgentFromContext(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("For-User-Agent")
		w.WriteHeader(http.StatusNoContent)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("test-service").
		Build(t.Context())
	require.NoError(t, err)

	ctx := httpc.ContextWithForUserAgent(t.Context(), "end-user-agent")
	_, _, err = httpc.NewGET[httpc.Void]("ForUserAgent", "/test").WithDecoder(httpc.VoidDecoder()).
		Call().Execute(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, "end-user-agent", got)
}

func TestEndpointExecute_ForUserAgentDoesNotOverrideHeader(t *testing.T) {
	var got string
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("For-User-Agent")
		w.WriteHeader(http.StatusNoContent)
	})

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("test-service").
		Build(t.Context())
	require.NoError(t, err)

	ctx := httpc.ContextWithForUserAgent(t.Context(), "context-value")
	_, _, err = httpc.NewGET[httpc.Void]("ForUserAgent", "/test").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("For-User-Agent", "request-value").
		Call().Execute(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, "request-value", got)
}

func TestEndpointExecute_CopyOnWrite(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	})

	base := httpc.NewNoBodyEndpoint[testPayload](http.MethodGet, "GetItems", "/items").
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	// Derive two variants from the same base.
	v1 := base.WithAddedHeader("X-Version", "1")
	v2 := base.WithAddedHeader("X-Version", "2")

	// Both should work independently.
	_, _, err := v1.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	_, _, err = v2.Call().Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestEndpointExecute_NoDecoder(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ignored"))
	})

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "NoDec", "/nodec")

	result, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, struct{}{}, result)
}

func TestEndpointExecute_BinaryDecoder(t *testing.T) {
	expected := "binary content stream"
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	})

	ep := httpc.NewNoBodyEndpoint[io.ReadCloser](http.MethodGet, "Download", "/download").
		WithDecoder(httpc.BinaryDecoder()).
		WithAccept("application/octet-stream")

	result, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	require.NotNil(t, result)
	defer func() { _ = result.Close() }()

	data, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

// Verify the testError type has the correct Error() output for the error decoder test.
func TestTestError(t *testing.T) {
	err := &testError{statusCode: 403, body: "forbidden"}
	assert.Contains(t, err.Error(), "Forbidden")
}

// TestNewMethodConstructors verifies each New<METHOD> constructor produces an
// endpoint whose request carries the matching HTTP method (and sends the encoded
// body for the body-taking constructors). Body encode/decode itself is covered by
// TestEndpointExecute_JSONRoundTrip, TestWithJSON, and codec_test.
func TestNewMethodConstructors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		exec   func(ctx context.Context, client httpc.Runtime) error
	}{
		{"GET", http.MethodGet, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewGET[struct{}]("Get", "/x").WithDecoder(httpc.VoidDecoder()).Call().Execute(ctx, client)
			return err
		}},
		{"DELETE", http.MethodDelete, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewDELETE[struct{}]("Del", "/x").WithDecoder(httpc.VoidDecoder()).Call().Execute(ctx, client)
			return err
		}},
		{"HEAD", http.MethodHead, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewHEAD[struct{}]("Head", "/x").WithDecoder(httpc.VoidDecoder()).Call().Execute(ctx, client)
			return err
		}},
		{"POST", http.MethodPost, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewPOST[testPayload, struct{}]("Post", "/x").WithEncoder(httpc.JSONEncoder[testPayload]()).WithDecoder(httpc.VoidDecoder()).Call(testPayload{Name: "x"}).Execute(ctx, client)
			return err
		}},
		{"PUT", http.MethodPut, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewPUT[testPayload, struct{}]("Put", "/x").WithEncoder(httpc.JSONEncoder[testPayload]()).WithDecoder(httpc.VoidDecoder()).Call(testPayload{Name: "x"}).Execute(ctx, client)
			return err
		}},
		{"PATCH", http.MethodPatch, func(ctx context.Context, client httpc.Runtime) error {
			_, _, err := httpc.NewPATCH[testPayload, struct{}]("Patch", "/x").WithEncoder(httpc.JSONEncoder[testPayload]()).WithDecoder(httpc.VoidDecoder()).Call(testPayload{Name: "x"}).Execute(ctx, client)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod string
			client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				w.WriteHeader(http.StatusNoContent)
			})
			require.NoError(t, tc.exec(context.Background(), client))
			assert.Equal(t, tc.method, gotMethod)
		})
	}
}

// TestWithJSON verifies the WithJSON sugar on both endpoint kinds: it sets the
// JSON decoder + Accept (and, for a body endpoint, the JSON encoder +
// Content-Type) and round-trips a typed value.
func TestWithJSON(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		body   bool
		exec   func(ctx context.Context, client httpc.Runtime) (testPayload, error)
	}{
		{"GET", http.MethodGet, false, func(ctx context.Context, client httpc.Runtime) (testPayload, error) {
			r, _, err := httpc.NewGET[testPayload]("GetItem", "/items/1").WithJSON().Call().Execute(ctx, client)
			return r, err
		}},
		{"POST", http.MethodPost, true, func(ctx context.Context, client httpc.Runtime) (testPayload, error) {
			r, _, err := httpc.NewPOST[testPayload, testPayload]("CreateItem", "/items").WithJSON().Call(testPayload{Name: "req", Value: 1}).Execute(ctx, client)
			return r, err
		}},
		{"PUT", http.MethodPut, true, func(ctx context.Context, client httpc.Runtime) (testPayload, error) {
			r, _, err := httpc.NewPUT[testPayload, testPayload]("UpdateItem", "/items/1").WithJSON().Call(testPayload{Name: "req", Value: 1}).Execute(ctx, client)
			return r, err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.method, r.Method)
				assert.Equal(t, "application/json", r.Header.Get("Accept"))
				if tc.body {
					assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
					b, _ := io.ReadAll(r.Body)
					assert.JSONEq(t, `{"name":"req","value":1}`, string(b))
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"name":"resp","value":2}`))
			})
			result, err := tc.exec(context.Background(), client)
			require.NoError(t, err)
			assert.Equal(t, testPayload{Name: "resp", Value: 2}, result)
		})
	}
}

// Tests for compression round-trips through endpoints

func TestEndpointExecute_CompressionRoundTrip(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "deflate", r.Header.Get("Content-Encoding"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Decompress the request body.
		reader, err := zlib.NewReader(r.Body)
		require.NoError(t, err)
		defer func() { _ = reader.Close() }()
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, `{"name":"compressed","value":42}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	})

	ep := httpc.NewPOST[testPayload, testPayload]("Compressed", "/test").
		WithEncoder(httpc.ZLIBEncoder(httpc.JSONEncoder[testPayload]())).
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Call(testPayload{Name: "compressed", Value: 42}).Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
}

// Test response body draining

func TestEndpointExecute_ResponseBodyDrained(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":1}`))
	})

	ep := httpc.NewGET[testPayload]("DrainTest", "/test").
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 1}, result)
}

// Test BinaryEncoder through endpoint (non-retryable stream)

func TestEndpointExecute_BinaryEncoderOnce(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "stream data", string(body))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewBodyEndpoint[io.ReadCloser, struct{}](http.MethodPost, "Upload", "/upload").
		WithEncoder(httpc.BinaryEncoder("application/octet-stream")).
		WithDecoder(httpc.VoidDecoder())

	body := io.NopCloser(strings.NewReader("stream data"))
	_, _, err := ep.Call(body).Execute(context.Background(), client)
	require.NoError(t, err)
}

// Test buffer pool from client using the full builder path

func TestEndpointExecute_BufferPoolFromClient(t *testing.T) {
	pool := newTrackingPool()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"pooled","value":7}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	}))
	t.Cleanup(server.Close)

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("pool-test").
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testPayload, testPayload]("PoolTest", "/test").
		WithEncoder(httpc.JSONEncoder[testPayload]()).
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json").
		WithBufferPool(pool)

	result, _, err := ep.Call(testPayload{Name: "pooled", Value: 7}).Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
	assert.Equal(t, 1, pool.gets, "pool should have been used for encoding")
	assert.Equal(t, 1, pool.puts, "buffer should have been returned to pool after body close")
}

// Verify JSONEncoder without pool still works (no context pool).
func TestEndpointExecute_NoBufferPool(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"nop","value":0}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	})

	ep := httpc.NewPOST[testPayload, testPayload]("NoPool", "/test").
		WithEncoder(httpc.JSONEncoder[testPayload]()).
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Call(testPayload{Name: "nop", Value: 0}).Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
}

// Ensure BinaryDecoder body is not drained (rawBodyDecoder)
func TestEndpointExecute_BinaryDecoderNotDrained(t *testing.T) {
	expected := "binary stream"
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	})

	ep := httpc.NewGET[io.ReadCloser]("Download", "/dl").
		WithDecoder(httpc.BinaryDecoder()).
		WithAccept("application/octet-stream")

	result, _, err := ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Body should still be readable.
	data, err := io.ReadAll(result)
	require.NoError(t, err)
	_ = result.Close()
	assert.Equal(t, expected, string(data))
}

// Test buffer pool with compression encoding through the builder.
func TestEndpointExecute_PoolWithCompression(t *testing.T) {
	pool := newTrackingPool()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "deflate", r.Header.Get("Content-Encoding"))

		compressed, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		// Decompress and verify.
		reader, err := zlib.NewReader(bytes.NewReader(compressed))
		require.NoError(t, err)
		defer func() { _ = reader.Close() }()
		body, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.JSONEq(t, `{"name":"pool-zlib","value":9}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	}))
	t.Cleanup(server.Close)

	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetServiceName("pool-zlib-test").
		DisableTracing().
		DisablePanicRecovery().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testPayload, testPayload]("PoolZlib", "/test").
		WithEncoder(httpc.ZLIBEncoder(httpc.JSONEncoder[testPayload]())).
		WithDecoder(httpc.JSONDecoder[testPayload]()).
		WithAccept("application/json").
		WithBufferPool(pool)

	result, _, err := ep.Call(testPayload{Name: "pool-zlib", Value: 9}).Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
	// Pool was used by JSONEncoder, and buffer was returned when compression read the body.
	assert.Equal(t, 1, pool.gets)
	assert.Equal(t, 1, pool.puts)
}

func TestEndpointExecute_UnpopulatedPathParam(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetItem", "/items/{itemId}").
		WithDecoder(httpc.VoidDecoder())

	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{itemId}")
	assert.Contains(t, err.Error(), "not populated")
}

func TestEndpointExecute_UnpopulatedGreedyPathParam(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetFile", "/files/{filePath*}").
		WithDecoder(httpc.VoidDecoder())

	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	_, _, err := ep.Call().Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{filePath*}")
	assert.Contains(t, err.Error(), "not populated")
}

func TestEndpointExecute_PartialPathParams(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetOrgItem", "/orgs/{orgId}/items/{itemId}").
		WithDecoder(httpc.VoidDecoder())
	// itemId is still unfilled

	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})

	_, _, err := ep.Call().WithPathParam("orgId", "acme").Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{itemId}")
	assert.Contains(t, err.Error(), "not populated")
}

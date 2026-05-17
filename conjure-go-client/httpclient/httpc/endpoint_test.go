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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// httpTestClient wraps an httptest.Server as an httpc.Client.
type httpTestClient struct {
	server *httptest.Server
}

func (c *httpTestClient) Do(req *http.Request) (*http.Response, error) {
	// Prepend the test server's URL to the request path.
	req.URL.Scheme = "http"
	req.URL.Host = c.server.Listener.Addr().String()
	return c.server.Client().Do(req)
}

// newTestServer creates an httptest.Server and registers cleanup.
func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestEndpointExecute_JSONRoundTrip(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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

	ep := httpc.NewEndpoint[testPayload, testPayload](http.MethodPost, "CreateItem", "/api/v1/items").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, testPayload{Name: "test", Value: 1})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "response", Value: 2}, result)
}

func TestEndpointExecute_Headers(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "val1", r.Header.Get("X-Custom"))
		assert.Equal(t, "val2", r.Header.Get("X-Other"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		SetDecoder(httpc.VoidDecoder()).
		AddHeader("X-Custom", "val1").
		AddHeader("X-Other", "val2")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_QueryParams(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Search", "/search").
		SetDecoder(httpc.VoidDecoder()).
		AddQuery("foo", "bar").
		AddQuery("page", "2")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_BasicAuth(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "myuser", user)
		assert.Equal(t, "mypass", pass)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Auth", "/auth").
		SetDecoder(httpc.VoidDecoder()).
		WithBasicAuth("myuser", "mypass")

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_BasicAuthOverridesClientAuth(t *testing.T) {
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
		SetAuthTokenProvider(func(context.Context) (string, error) {
			providerCalled = true
			return "", assert.AnError
		}).
		SetTransport(transport).
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Auth", "/auth").
		SetDecoder(httpc.VoidDecoder()).
		WithBasicAuth("request-user", "request-pass")

	_, _, err = ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "Basic cmVxdWVzdC11c2VyOnJlcXVlc3QtcGFzcw==", gotAuth)
	assert.False(t, providerCalled)
}

func TestEndpointExecute_Timeout(t *testing.T) {
	// Verify the timeout is set by checking that a slow server causes an error.
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Slow", "/slow").
		SetDecoder(httpc.VoidDecoder()).
		WithTimeout(50 * time.Millisecond)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestEndpointExecute_ZeroTimeoutDoesNotCancelContext(t *testing.T) {
	client := clientFunc(func(req *http.Request) (*http.Response, error) {
		require.NoError(t, req.Context().Err())
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    req,
		}, nil
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		SetDecoder(httpc.VoidDecoder()).
		WithTimeout(0)

	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_ErrorDecoder(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Err", "/err").
		SetDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(&testErrorDecoder{})

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test error: Forbidden")
}

func TestEndpointExecute_ErrorDecoderFallsBackToClientDecoder(t *testing.T) {
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusForbidden,
			Status:     "403 Forbidden",
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("forbidden")),
			Request:    req,
		}, nil
	}}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetTransport(transport).
		SetErrorDecoder(&testErrorDecoder{}).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Err", "/err").
		SetDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(neverErrorDecoder{})

	_, _, err = ep.Execute(context.Background(), client, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "test error: Forbidden")
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

type neverErrorDecoder struct{}

func (neverErrorDecoder) Handles(*http.Response) bool { return false }

func (neverErrorDecoder) DecodeError(*http.Response) error {
	return assert.AnError
}

type clientFunc func(*http.Request) (*http.Response, error)

func (f clientFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

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
		DisableRestErrors().
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("GetItem", "/items/{itemId}").
		SetDecoder(httpc.VoidDecoder()).
		WithPathParam("itemId", "a/b")

	_, _, err = ep.Execute(context.Background(), client, httpc.Void{})
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

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "MW", "/mw").
		SetDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.True(t, middlewareCalled)
}

func TestEndpointExecute_Void(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[httpc.Void, httpc.Void](http.MethodDelete, "DeleteItem", "/item/1").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
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

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "MyRPCMethod", "/rpc").
		SetDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, struct{}{})
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
		DisableRestErrors().
		Build(t.Context())
	require.NoError(t, err)

	ctx := httpc.ContextWithForUserAgent(t.Context(), "end-user-agent")
	_, _, err = httpc.NewGET[httpc.Void]("ForUserAgent", "/test").SetDecoder(httpc.VoidDecoder()).
		Execute(ctx, client, httpc.Void{})
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
		DisableRestErrors().
		Build(t.Context())
	require.NoError(t, err)

	ctx := httpc.ContextWithForUserAgent(t.Context(), "context-value")
	_, _, err = httpc.NewGET[httpc.Void]("ForUserAgent", "/test").
		SetDecoder(httpc.VoidDecoder()).
		SetHeader("For-User-Agent", "request-value").
		Execute(ctx, client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, "request-value", got)
}

func TestEndpointExecute_CopyOnWrite(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	})

	base := httpc.NewEndpoint[struct{}, testPayload](http.MethodGet, "GetItems", "/items").
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	// Derive two variants from the same base.
	v1 := base.AddHeader("X-Version", "1")
	v2 := base.AddHeader("X-Version", "2")

	client := &httpTestClient{server: server}

	// Both should work independently.
	_, _, err := v1.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	_, _, err = v2.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_NoDecoder(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ignored"))
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "NoDec", "/nodec")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, struct{}{}, result)
}

func TestEndpointExecute_BinaryDecoder(t *testing.T) {
	expected := "binary content stream"
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	})

	ep := httpc.NewEndpoint[struct{}, io.ReadCloser](http.MethodGet, "Download", "/download").
		SetDecoder(httpc.BinaryDecoder()).
		SetAccept("application/octet-stream")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, struct{}{})
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

// Tests for void constructors (NewGET, NewDELETE, etc.)

func TestNewGET(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"get","value":1}`))
	})

	ep := httpc.NewGET[testPayload]("GetItems", "/items").
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "get", Value: 1}, result)
}

func TestNewDELETE(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewDELETE[struct{}]("DeleteItem", "/item/1").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.NoError(t, err)
}

func TestNewHEAD(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.WriteHeader(http.StatusOK)
	})

	ep := httpc.NewHEAD[struct{}]("Health", "/health").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.NoError(t, err)
}

func TestNewPOST(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"post","value":1}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"created","value":2}`))
	})

	ep := httpc.NewPOST[testPayload, testPayload]("CreateItem", "/items").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, testPayload{Name: "post", Value: 1})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "created", Value: 2}, result)
}

func TestNewPUT(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewPUT[testPayload, struct{}]("UpdateItem", "/item/1").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, testPayload{Name: "put", Value: 1})
	require.NoError(t, err)
}

func TestNewPATCH(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewPATCH[testPayload, struct{}]("PatchItem", "/item/1").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, _, err := ep.Execute(context.Background(), client, testPayload{Name: "patch", Value: 1})
	require.NoError(t, err)
}

// Tests for JSON convenience constructors

func TestNewJSONGET(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"json-get","value":1}`))
	})

	client := &httpTestClient{server: server}
	result, _, err := httpc.NewJSONGET[testPayload]("GetItem", "/items/1").
		Execute(context.Background(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "json-get", Value: 1}, result)
}

func TestNewJSONPOST(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"req","value":1}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"resp","value":2}`))
	})

	client := &httpTestClient{server: server}
	result, _, err := httpc.NewJSONPOST[testPayload, testPayload]("CreateItem", "/items").
		Execute(context.Background(), client, testPayload{Name: "req", Value: 1})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "resp", Value: 2}, result)
}

func TestNewJSONPUT(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"update","value":3}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"updated","value":4}`))
	})

	client := &httpTestClient{server: server}
	result, _, err := httpc.NewJSONPUT[testPayload, testPayload]("UpdateItem", "/items/1").
		Execute(context.Background(), client, testPayload{Name: "update", Value: 3})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "updated", Value: 4}, result)
}

// Tests for compression round-trips through endpoints

func TestEndpointExecute_CompressionRoundTrip(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
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
		SetEncoder(httpc.ZLIBEncoder(httpc.JSONEncoder[testPayload]())).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, testPayload{Name: "compressed", Value: 42})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
}

// Test response body draining

func TestEndpointExecute_ResponseBodyDrained(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":1}`))
	})

	ep := httpc.NewGET[testPayload]("DrainTest", "/test").
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 1}, result)
}

// Test BinaryEncoder through endpoint (non-retryable stream)

func TestEndpointExecute_BinaryEncoderOnce(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, "stream data", string(body))
		w.WriteHeader(http.StatusNoContent)
	})

	ep := httpc.NewEndpoint[io.ReadCloser, struct{}](http.MethodPost, "Upload", "/upload").
		SetEncoder(httpc.BinaryEncoder("application/octet-stream")).
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	body := io.NopCloser(strings.NewReader("stream data"))
	_, _, err := ep.Execute(context.Background(), client, body)
	require.NoError(t, err)
}

// Test buffer pool from client using the full builder path.

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
		SetBytesBufferPool(pool).
		DisableTracing().
		DisablePanicRecovery().
		DisableRestErrors().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testPayload, testPayload]("PoolTest", "/test").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	result, _, err := ep.Execute(t.Context(), client, testPayload{Name: "pooled", Value: 7})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
	assert.Equal(t, 1, pool.gets, "pool should have been used for encoding")
	assert.Equal(t, 1, pool.puts, "buffer should have been returned to pool after body close")
}

// Verify JSONEncoder without pool still works (no context pool).
func TestEndpointExecute_NoBufferPool(t *testing.T) {
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"nop","value":0}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	})

	ep := httpc.NewPOST[testPayload, testPayload]("NoPool", "/test").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, testPayload{Name: "nop", Value: 0})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
}

// Ensure BinaryDecoder body is not drained (rawBodyDecoder)
func TestEndpointExecute_BinaryDecoderNotDrained(t *testing.T) {
	expected := "binary stream"
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	})

	ep := httpc.NewGET[io.ReadCloser]("Download", "/dl").
		SetDecoder(httpc.BinaryDecoder()).
		SetAccept("application/octet-stream")

	client := &httpTestClient{server: server}
	result, _, err := ep.Execute(context.Background(), client, httpc.Void{})
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
		SetBytesBufferPool(pool).
		DisableTracing().
		DisablePanicRecovery().
		DisableRestErrors().
		Build(t.Context())
	require.NoError(t, err)

	ep := httpc.NewPOST[testPayload, testPayload]("PoolZlib", "/test").
		SetEncoder(httpc.ZLIBEncoder(httpc.JSONEncoder[testPayload]())).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	result, _, err := ep.Execute(t.Context(), client, testPayload{Name: "pool-zlib", Value: 9})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "ok", Value: 0}, result)
	// Pool was used by JSONEncoder, and buffer was returned when compression read the body.
	assert.Equal(t, 1, pool.gets)
	assert.Equal(t, 1, pool.puts)
}

func TestEndpointExecute_UnpopulatedPathParam(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetItem", "/items/{itemId}").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})}

	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{itemId}")
	assert.Contains(t, err.Error(), "not populated")
}

func TestEndpointExecute_UnpopulatedGreedyPathParam(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetFile", "/files/{filePath*}").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})}

	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{filePath*}")
	assert.Contains(t, err.Error(), "not populated")
}

func TestEndpointExecute_PartialPathParams(t *testing.T) {
	ep := httpc.NewGET[struct{}]("GetOrgItem", "/orgs/{orgId}/items/{itemId}").
		SetDecoder(httpc.VoidDecoder()).
		WithPathParam("orgId", "acme")
	// itemId is still unfilled.

	client := &httpTestClient{server: newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	})}

	_, _, err := ep.Execute(context.Background(), client, httpc.Void{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "{itemId}")
	assert.Contains(t, err.Error(), "not populated")
}

package httpc_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestEndpointExecute_JSONRoundTrip(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/items", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "application/json", r.Header.Get("Accept"))

		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"name":"test","value":1}`, string(body))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"response","value":2}`))
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[testPayload, testPayload](http.MethodPost, "/api/v1/items", "CreateItem").
		SetEncoder(httpc.JSONEncoder[testPayload]()).
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	client := &httpTestClient{server: server}
	result, err := ep.Execute(context.Background(), client, testPayload{Name: "test", Value: 1})
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "response", Value: 2}, result)
}

func TestEndpointExecute_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "val1", r.Header.Get("X-Custom"))
		assert.Equal(t, "val2", r.Header.Get("X-Other"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/test", "Test").
		SetDecoder(httpc.VoidDecoder()).
		WithHeader("X-Custom", "val1").
		WithHeader("X-Other", "val2")

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		assert.Equal(t, "2", r.URL.Query().Get("page"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/search", "Search").
		SetDecoder(httpc.VoidDecoder()).
		WithQueryParam("foo", "bar").
		WithQueryParam("page", "2")

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_BasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "myuser", user)
		assert.Equal(t, "mypass", pass)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/auth", "Auth").
		SetDecoder(httpc.VoidDecoder()).
		WithBasicAuth("myuser", "mypass")

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_Timeout(t *testing.T) {
	// Verify the timeout is set by checking that a slow server causes an error.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/slow", "Slow").
		SetDecoder(httpc.VoidDecoder()).
		WithTimeout(50 * time.Millisecond)

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestEndpointExecute_ErrorDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/err", "Err").
		SetDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(&testErrorDecoder{})

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
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

func TestEndpointExecute_Middleware(t *testing.T) {
	var middlewareCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "middleware-value", r.Header.Get("X-Middleware"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		middlewareCalled = true
		req.Header.Set("X-Middleware", "middleware-value")
		return next.RoundTrip(req)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/mw", "MW").
		SetDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.True(t, middlewareCalled)
}

func TestExecuteVoid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodDelete, "/item/1", "DeleteItem").
		SetDecoder(httpc.VoidDecoder())

	client := &httpTestClient{server: server}
	_, err := httpc.ExecuteVoid(context.Background(), client, ep)
	require.NoError(t, err)
}

func TestEndpointExecute_RPCMethodName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	var capturedCtx context.Context
	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		capturedCtx = req.Context()
		return next.RoundTrip(req)
	})

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/rpc", "MyRPCMethod").
		SetDecoder(httpc.VoidDecoder()).
		WithMiddleware(mw)

	client := &httpTestClient{server: server}
	_, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)

	name, ok := httpc.RPCMethodName(capturedCtx)
	assert.True(t, ok)
	assert.Equal(t, "MyRPCMethod", name)
}

func TestEndpointExecute_CopyOnWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"ok","value":0}`))
	}))
	defer server.Close()

	base := httpc.NewEndpoint[struct{}, testPayload](http.MethodGet, "/items", "GetItems").
		SetDecoder(httpc.JSONDecoder[testPayload]()).
		SetAccept("application/json")

	// Derive two variants from the same base.
	v1 := base.WithHeader("X-Version", "1")
	v2 := base.WithHeader("X-Version", "2")

	client := &httpTestClient{server: server}

	// Both should work independently.
	_, err := v1.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	_, err = v2.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
}

func TestEndpointExecute_NoDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ignored"))
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "/nodec", "NoDec")

	client := &httpTestClient{server: server}
	result, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, struct{}{}, result)
}

func TestEndpointExecute_BinaryDecoder(t *testing.T) {
	expected := "binary content stream"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	}))
	defer server.Close()

	ep := httpc.NewEndpoint[struct{}, io.ReadCloser](http.MethodGet, "/download", "Download").
		SetDecoder(httpc.BinaryDecoder()).
		SetAccept("application/octet-stream")

	client := &httpTestClient{server: server}
	result, err := ep.Execute(context.Background(), client, struct{}{})
	require.NoError(t, err)
	require.NotNil(t, result)
	defer result.Close()

	data, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

// Verify the testError type has the correct Error() output for the error decoder test.
func TestTestError(t *testing.T) {
	err := &testError{statusCode: 403, body: "forbidden"}
	assert.Contains(t, err.Error(), "Forbidden")
}

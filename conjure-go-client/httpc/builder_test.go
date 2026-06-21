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
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type builderTestPayload struct {
	Message string `json:"message"`
}

func TestBuilder_BasicBuild(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"hello"}`))
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("test-service").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, builderTestPayload](http.MethodGet, "GetTest", "/api/test").
		WithDecoder(httpc.JSONDecoder[builderTestPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Message)
}

func TestBuilder_Clone(t *testing.T) {
	b1 := httpc.NewBuilder().
		SetServiceName("svc1").
		SetBaseURLs("http://localhost:1234").
		SetTimeout(5 * time.Second)

	b2 := b1.Clone()
	b2.SetServiceName("svc2")
	b2.SetTimeout(10 * time.Second)

	// Both should build independently.
	b1.SetAllowCreateWithEmptyURIs(true)
	b2.SetAllowCreateWithEmptyURIs(true)
	_, err := b1.Build(context.Background())
	require.NoError(t, err)
	_, err = b2.Build(context.Background())
	require.NoError(t, err)
}

func TestBuilder_Apply(t *testing.T) {
	setCustomTimeout := func(b *httpc.Builder) *httpc.Builder {
		return b.SetTimeout(30 * time.Second)
	}

	b := httpc.NewBuilder().
		SetBaseURLs("http://localhost:1234").
		Apply(setCustomTimeout)

	_, err := b.Build(context.Background())
	require.NoError(t, err)
}

func TestBuilder_RebuildableRuntime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"reconfigured"}`))
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("test-service").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	// Builder() should return a usable builder.
	builder := client.Builder()
	require.NotNil(t, builder)

	// Build a new client from the builder.
	client2, err := builder.
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, builderTestPayload](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.JSONDecoder[builderTestPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Execute(context.Background(), client2)
	require.NoError(t, err)
	assert.Equal(t, "reconfigured", result.Message)
}

// TestBuilder_BuilderSnapshotsAtBuildTime verifies that
// client.Builder() returns a clone of the builder as it was at Build time,
// not the current (potentially mutated) builder.
func TestBuilder_BuilderSnapshotsAtBuildTime(t *testing.T) {
	b := httpc.NewBuilder().
		SetServiceName("original").
		SetBaseURLs("http://localhost:1234").
		SetTimeout(5 * time.Second)

	client, err := b.Build(context.Background())
	require.NoError(t, err)

	// Mutate the builder after Build.
	b.SetTimeout(99 * time.Second)

	// Builder() should return a snapshot from Build time (5s), not the
	// current builder state (99s).
	rebuilder := client.Builder()
	httpClient, err := rebuilder.BuildHTTPClient(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, httpClient.Current().Timeout)
}

func TestBuilder_Middleware(t *testing.T) {
	var middlewareSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-value", r.Header.Get("X-Test"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		middlewareSeen = true
		req.Header.Set("X-Test", "test-value")
		return next.RoundTrip(req)
	})

	client, err := httpc.NewBuilder().
		SetServiceName("mw-service").
		SetBaseURLs(server.URL).
		AddMiddleware(mw).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "MW", "/mw").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.True(t, middlewareSeen)
}

func TestBuilder_PostWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.JSONEq(t, `{"message":"request"}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"response"}`))
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("post-service").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[builderTestPayload, builderTestPayload](http.MethodPost, "Create", "/create").
		WithEncoder(httpc.JSONEncoder[builderTestPayload]()).
		WithDecoder(httpc.JSONDecoder[builderTestPayload]()).
		WithAccept("application/json")

	result, _, err := ep.WithBody(builderTestPayload{Message: "request"}).Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "response", result.Message)
}

func TestBuilder_EmptyURIsError(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetServiceName("no-urls").
		Build(context.Background())
	require.Error(t, err)
}

func TestBuilder_AllowEmptyURIs(t *testing.T) {
	_, err := httpc.NewBuilder().
		SetServiceName("empty-ok").
		SetBaseURLs().
		SetAllowCreateWithEmptyURIs(true).
		Build(context.Background())
	require.NoError(t, err)
}

func TestBuilder_ErrorDecoder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("err-service").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Fail", "/fail").
		WithDecoder(httpc.VoidDecoder()).
		WithErrorDecoder(&builderTestErrorDecoder{})

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "builder-decoded error")
}

type builderTestErrorDecoder struct{}

func (d *builderTestErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= 400
}

func (d *builderTestErrorDecoder) DecodeError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	return &builderTestError{status: resp.StatusCode, body: string(body)}
}

type builderTestError struct {
	status int
	body   string
}

func (e *builderTestError) Error() string {
	return "builder-decoded error"
}

func TestBuilder_Headers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "custom-agent", r.Header.Get("User-Agent"))
		assert.Equal(t, "val1", r.Header.Get("X-Custom"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("header-service").
		SetBaseURLs(server.URL).
		SetUserAgent("custom-agent").
		AddHeader("X-Custom", "val1").
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Headers", "/headers").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
}

func TestBuilder_DefaultErrorDecoder_StatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("err-svc").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Fail", "/fail").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)

	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusInternalServerError, statusCode)
}

func TestBuilder_DefaultErrorDecoder_NonJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("plain text error"))
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("err-svc").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Fail", "/fail").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400 Bad Request")

	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusBadRequest, statusCode)
	// Body is included as an unsafe param (not visible in Error() string).
}

func TestBuilder_DefaultErrorDecoder_ConjureJSON(t *testing.T) {
	conjureError := map[string]any{
		"errorCode":       "NOT_FOUND",
		"errorName":       "Default:NotFound",
		"errorInstanceId": "00000000-0000-0000-0000-000000000000",
		"parameters":      map[string]any{},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(conjureError)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("err-svc").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "GetMissing", "/missing").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)

	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusNotFound, statusCode)

	// The error should contain the Conjure error name.
	assert.Contains(t, err.Error(), "NOT_FOUND")
}

func TestBuilder_DefaultErrorDecoder_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("err-svc").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Down", "/down").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "503 Service Unavailable")

	statusCode, ok := httpc.StatusCodeFromError(err)
	assert.True(t, ok)
	assert.Equal(t, http.StatusServiceUnavailable, statusCode)
}

func TestBuilder_BuildDialer(t *testing.T) {
	dialer, err := httpc.NewBuilder().
		SetDialTimeout(5 * time.Second).
		SetKeepAlive(10 * time.Second).
		BuildDialer(context.Background())
	require.NoError(t, err)
	require.NotNil(t, dialer)

	// Verify the dialer can connect to a test server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	conn, err := dialer.DialContext(context.Background(), "tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	_ = conn.Close()
}

func TestBuilder_BuildTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"transport"}`))
	}))
	defer server.Close()

	transport, err := httpc.NewBuilder().
		SetMaxIdleConnsPerHost(10).
		BuildTransport(context.Background())
	require.NoError(t, err)
	require.NotNil(t, transport)

	// Use the transport directly in an http.Client to verify it works.
	client := &http.Client{Transport: transport}
	resp, err := client.Get(server.URL + "/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"message":"transport"}`, string(body))
}

func TestBuilder_BuildTransport_EscapeHatch(t *testing.T) {
	var customTransportUsed bool
	customRT := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		customTransportUsed = true
		return next.RoundTrip(req)
	})

	// Wrap the default transport with our tracking middleware.
	transport := &trackingTransport{
		flag: &customTransportUsed,
		base: http.DefaultTransport,
	}

	rt, err := httpc.NewBuilder().
		SetTransport(transport).
		BuildTransport(context.Background())
	require.NoError(t, err)

	// SetTransport should bypass TLS/dialer building and return the injected transport.
	assert.Equal(t, transport, rt, "BuildTransport should return the injected transport")
	_ = customRT // avoid unused
}

type trackingTransport struct {
	flag *bool
	base http.RoundTripper
}

func (t *trackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	*t.flag = true
	return t.base.RoundTrip(req)
}

func TestBuilder_BuildHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"httpclient"}`))
	}))
	defer server.Close()

	httpClient, err := httpc.NewBuilder().
		BuildHTTPClient(context.Background())
	require.NoError(t, err)
	require.NotNil(t, httpClient)

	// Use the refreshable http.Client directly.
	client := httpClient.Current()
	resp, err := client.Get(server.URL + "/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestBuilder_BuildTLSConfig(t *testing.T) {
	tlsConfig, err := httpc.NewBuilder().
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)
	require.NotNil(t, tlsConfig)

	validated, validErr := tlsConfig.Validation()
	require.NoError(t, validErr)
	assert.True(t, validated.InsecureSkipVerify)
}

// TestRefreshable_TimeoutPropagation verifies that updating a refreshable timeout
// propagates to the *http.Client produced by BuildHTTPClient.
func TestRefreshable_TimeoutPropagation(t *testing.T) {
	timeout := refreshable.New(5 * time.Second)
	httpClient, err := httpc.NewBuilder().
		SetTimeoutRefreshable(timeout).
		BuildHTTPClient(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 5*time.Second, httpClient.Current().Timeout)

	// Update timeout — the refreshable.Map in BuildHTTPClient should
	// produce a new *http.Client with the updated timeout.
	timeout.Update(10 * time.Second)
	assert.Equal(t, 10*time.Second, httpClient.Current().Timeout)
}

// TestRefreshable_URIPropagation verifies that updating a refreshable URI list
// causes the built client to route requests to the new server.
func TestRefreshable_URIPropagation(t *testing.T) {
	var server1Hits, server2Hits int
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server1Hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server1.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		server2Hits++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server2.Close()

	uris := refreshable.New([]string{server1.URL})
	client, err := httpc.NewBuilder().
		SetBaseURLsRefreshable(uris).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, struct{}](http.MethodGet, "Test", "/test").
		WithDecoder(httpc.VoidDecoder())

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)
	assert.Equal(t, 0, server2Hits)

	// Update URIs to point to server2.
	uris.Update([]string{server2.URL})

	_, _, err = ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 1, server1Hits)
	assert.Equal(t, 1, server2Hits)
}

// sentinelDialer is a probe ContextDialer; the test only asserts BuildDialer
// returns this exact value.
type sentinelDialer struct{ net.Dialer }

// TestBuilder_SetDialer_ShortCircuits verifies SetDialer makes BuildDialer
// return the caller-provided dialer as-is and ignore SetDialTimeout etc.
func TestBuilder_SetDialer_ShortCircuits(t *testing.T) {
	custom := &sentinelDialer{}
	dialer, err := httpc.NewBuilder().
		SetDialTimeout(99 * time.Hour). // would otherwise show up in the built dialer
		SetDialer(custom).
		BuildDialer(context.Background())
	require.NoError(t, err)
	assert.Same(t, custom, dialer, "BuildDialer should return the SetDialer value unchanged")
}

// TestBuilder_SetTransport_FullClientPathWithCustomRoundTripper exercises the
// "use httpc for everything except the transport" pattern, e.g. an httptest
// recorder calling a handler directly without going through TCP. The
// middleware stack (auth, metrics, tracing, retry) still wraps the custom
// transport.
func TestBuilder_SetTransport_FullClientPathWithCustomRoundTripper(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"), "auth middleware should still run")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"in-process"}`))
	})

	// roundTripperFn calls the handler in-process via httptest.NewRecorder.
	var calls int
	rt := roundTripperFn(func(req *http.Request) (*http.Response, error) {
		calls++
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Result(), nil
	})

	client, err := httpc.NewBuilder().
		SetServiceName("in-process").
		SetBaseURLs("http://example.invalid").
		SetAuthToken("test-token").
		SetTransport(rt).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewEndpoint[struct{}, builderTestPayload](http.MethodGet, "GetTest", "/api/test").
		WithDecoder(httpc.JSONDecoder[builderTestPayload]()).
		WithAccept("application/json")

	result, _, err := ep.Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "custom transport should have been invoked exactly once")
	assert.Equal(t, "in-process", result.Message)
}

type roundTripperFn func(*http.Request) (*http.Response, error)

func (f roundTripperFn) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

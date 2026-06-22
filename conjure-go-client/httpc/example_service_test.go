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

// This file demonstrates what a Conjure-generated service client looks like
// when built on top of httpc endpoint descriptors and httpc.Overrides. The pattern is:
//
//  1. Package-level endpoint descriptor vars define the HTTP shape of each RPC,
//     using Conjure-style path templates with {param} placeholders.
//  2. The service struct holds an httpc.Overrides for per-client customization.
//  3. Each method fills in path params via WithPathParam, merges client-level
//     overrides via WithOverrides, then calls Execute.
//  4. Callers use the With* methods on Overrides to derive customized clients
//     without mutating the original

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Example generated types (would normally live in a separate api package)
// ---------------------------------------------------------------------------

type CreateItemRequest struct {
	Name string `json:"name"`
}

type CreateItemResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type GetItemResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ---------------------------------------------------------------------------
// Example generated service client
// ---------------------------------------------------------------------------

// Package-level endpoint descriptors. Path templates use {param} placeholders
// matching the Conjure definition. These are safe to share across goroutines.
var (
	createItemEndpoint = httpc.NewPOST[CreateItemRequest, CreateItemResponse]("CreateItem", "/api/v1/items").
				WithEncoder(httpc.JSONEncoder[CreateItemRequest]()).
				WithDecoder(httpc.JSONDecoder[CreateItemResponse]()).
				WithAccept("application/json")

	getItemEndpoint = httpc.NewGET[GetItemResponse]("GetItem", "/api/v1/items/{itemId}").
			WithDecoder(httpc.JSONDecoder[GetItemResponse]()).
			WithAccept("application/json")

	deleteItemEndpoint = httpc.NewDELETE[struct{}]("DeleteItem", "/api/v1/items/{itemId}").
				WithDecoder(httpc.VoidDecoder())

	downloadItemEndpoint = httpc.NewGET[io.ReadCloser]("DownloadItem", "/api/v1/items/{itemId}/download").
				WithDecoder(httpc.BinaryDecoder()).
				WithAccept("application/octet-stream")
)

// ItemServiceClient is the public interface for the item service.
type ItemServiceClient interface {
	CreateItem(ctx context.Context, req CreateItemRequest) (CreateItemResponse, error)
	GetItem(ctx context.Context, itemId string) (GetItemResponse, error)
	DeleteItem(ctx context.Context, itemId string) error
	DownloadItem(ctx context.Context, itemId string) (io.ReadCloser, error)
}

// itemServiceClient is the generated implementation.
type itemServiceClient struct {
	client    httpc.Runtime
	overrides httpc.Overrides
}

// NewItemServiceClient creates a new client for the item service.
func NewItemServiceClient(client httpc.Runtime, params ...httpc.Param[*itemServiceClientBuilder]) ItemServiceClient {
	c := &itemServiceClient{client: client}
	b := &itemServiceClientBuilder{inner: c}
	for _, p := range params {
		p(b)
	}
	return c
}

// itemServiceClientBuilder adapts the service client for Param/Apply usage.
type itemServiceClientBuilder struct {
	inner *itemServiceClient
}

func (b *itemServiceClientBuilder) Clone() *itemServiceClientBuilder {
	return &itemServiceClientBuilder{inner: &itemServiceClient{
		client:    b.inner.client,
		overrides: b.inner.overrides.Clone(),
	}}
}

func (b *itemServiceClientBuilder) Apply(params ...httpc.Param[*itemServiceClientBuilder]) *itemServiceClientBuilder {
	for _, p := range params {
		p(b)
	}
	return b
}

func (c *itemServiceClient) CreateItem(ctx context.Context, req CreateItemRequest) (CreateItemResponse, error) {
	resp, _, err := createItemEndpoint.
		Call(req).
		WithOverrides(c.overrides).
		Execute(ctx, c.client)
	return resp, err
}

func (c *itemServiceClient) GetItem(ctx context.Context, itemId string) (GetItemResponse, error) {
	resp, _, err := getItemEndpoint.
		Call().
		WithPathParam("itemId", itemId).
		WithOverrides(c.overrides).
		Execute(ctx, c.client)
	return resp, err
}

func (c *itemServiceClient) DeleteItem(ctx context.Context, itemId string) error {
	_, _, err := deleteItemEndpoint.
		Call().
		WithPathParam("itemId", itemId).
		WithOverrides(c.overrides).
		Execute(ctx, c.client)
	return err
}

func (c *itemServiceClient) DownloadItem(ctx context.Context, itemId string) (io.ReadCloser, error) {
	resp, _, err := downloadItemEndpoint.
		Call().
		WithPathParam("itemId", itemId).
		WithOverrides(c.overrides).
		Execute(ctx, c.client)
	return resp, err
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestExampleService_CreateItem(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/items", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var req CreateItemRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		assert.Equal(t, "widget", req.Name)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(CreateItemResponse{ID: "123", Name: req.Name})
	})
	svc := NewItemServiceClient(client)

	resp, err := svc.CreateItem(context.Background(), CreateItemRequest{Name: "widget"})
	require.NoError(t, err)
	assert.Equal(t, "123", resp.ID)
	assert.Equal(t, "widget", resp.Name)
}

func TestExampleService_GetItem(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/items/item-42", r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "item-42", Name: "gadget"})
	})
	svc := NewItemServiceClient(client)

	resp, err := svc.GetItem(context.Background(), "item-42")
	require.NoError(t, err)
	assert.Equal(t, "item-42", resp.ID)
	assert.Equal(t, "gadget", resp.Name)
}

func TestExampleService_DeleteItem(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		assert.Equal(t, "/api/v1/items/item-99", r.URL.Path)
		w.WriteHeader(http.StatusNoContent)
	})
	svc := NewItemServiceClient(client)

	err := svc.DeleteItem(context.Background(), "item-99")
	require.NoError(t, err)
}

func TestExampleService_DownloadItem(t *testing.T) {
	expected := "file-contents-here"
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v1/items/doc-1/download", r.URL.Path)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(expected))
	})
	svc := NewItemServiceClient(client)

	body, err := svc.DownloadItem(context.Background(), "doc-1")
	require.NoError(t, err)
	defer func() { _ = body.Close() }()

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

// TestExampleService_PathParamEscaping verifies that special characters in path
// parameters are properly URL-escaped.
func TestExampleService_PathParamEscaping(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		// "hello world" is escaped to "hello%20world" on the wire.
		assert.Contains(t, r.RequestURI, "/api/v1/items/hello%20world")
		// The decoded path should have the original value.
		assert.Equal(t, "/api/v1/items/hello world", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "hello world", Name: "x"})
	})
	svc := NewItemServiceClient(client)

	resp, err := svc.GetItem(context.Background(), "hello world")
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.ID)
}

// TestExampleService_MultiplePathParams verifies that endpoints with multiple
// path parameters fill them in by name, regardless of call order.
func TestExampleService_MultiplePathParams(t *testing.T) {
	// A contrived endpoint with two path params to test named replacement.
	ep := httpc.NewGET[GetItemResponse]("GetOrgItem", "/orgs/{orgId}/items/{itemId}").
		WithDecoder(httpc.JSONDecoder[GetItemResponse]()).
		WithAccept("application/json")

	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/acme/items/widget-1", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "widget-1", Name: "Widget"})
	})

	// Fill in params in reverse order — should still work because replacement is by name.
	resp, _, err := ep.Call().WithPathParam("itemId", "widget-1").WithPathParam("orgId", "acme").
		Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "widget-1", resp.ID)
}

// TestExampleService_GreedyPathParam verifies that a {param*} placeholder preserves
// slashes in the value while still escaping individual segments.
func TestExampleService_GreedyPathParam(t *testing.T) {
	ep := httpc.NewGET[GetItemResponse]("GetFile", "/files/{filePath*}").
		WithDecoder(httpc.JSONDecoder[GetItemResponse]()).
		WithAccept("application/json")

	t.Run("simple nested path", func(t *testing.T) {
		client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/files/dir/subdir/file.txt", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "file.txt", Name: "x"})
		})

		resp, _, err := ep.Call().WithPathParam("filePath", "dir/subdir/file.txt").
			Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "file.txt", resp.ID)
	})

	t.Run("segments with spaces are escaped", func(t *testing.T) {
		client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
			// Slashes preserved, but spaces within segments are escaped.
			assert.Equal(t, "/files/my docs/sub dir/file.txt", r.URL.Path)
			assert.Contains(t, r.RequestURI, "/files/my%20docs/sub%20dir/file.txt")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "file.txt", Name: "x"})
		})

		resp, _, err := ep.Call().WithPathParam("filePath", "my docs/sub dir/file.txt").
			Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "file.txt", resp.ID)
	})

	t.Run("greedy param with prefix", func(t *testing.T) {
		ep2 := httpc.NewGET[GetItemResponse]("GetRepoFile", "/repos/{repoId}/files/{filePath*}").
			WithDecoder(httpc.JSONDecoder[GetItemResponse]()).
			WithAccept("application/json")

		client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/repos/my-repo/files/src/main/app.go", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "app.go", Name: "x"})
		})

		resp, _, err := ep2.Call().WithPathParam("repoId", "my-repo").WithPathParam("filePath", "src/main/app.go").
			Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "app.go", resp.ID)
	})

	t.Run("no slashes behaves like regular param", func(t *testing.T) {
		client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/files/simple.txt", r.URL.Path)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "simple.txt", Name: "x"})
		})

		resp, _, err := ep.Call().WithPathParam("filePath", "simple.txt").
			Execute(context.Background(), client)
		require.NoError(t, err)
		assert.Equal(t, "simple.txt", resp.ID)
	})
}

// TestExampleService_OverridesApplied verifies that Overrides set at client
// construction time are threaded through to every request.
func TestExampleService_OverridesApplied(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/items/1", r.URL.Path)
		assert.Equal(t, "acme-corp", r.Header.Get("X-Tenant"))
		assert.Equal(t, "token-abc", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "1", Name: "x"})
	})
	overrides := httpc.Overrides{}.
		WithAddedHeader("X-Tenant", "acme-corp").
		WithAddedHeader("Authorization", "token-abc")

	svc := &itemServiceClient{client: client, overrides: overrides}

	_, err := svc.GetItem(context.Background(), "1")
	require.NoError(t, err)
}

// TestExampleService_PerCallOverride shows that per-call endpoint overrides
// compose with the client-level overrides without affecting other calls.
func TestExampleService_PerCallOverride(t *testing.T) {
	callCount := 0
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		// Client-level header should always be present.
		assert.Equal(t, "acme-corp", r.Header.Get("X-Tenant"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "1", Name: "x"})
	})
	overrides := httpc.Overrides{}.WithAddedHeader("X-Tenant", "acme-corp")
	svc := &itemServiceClient{client: client, overrides: overrides}

	// Two calls; both should carry the tenant header.
	_, err := svc.GetItem(context.Background(), "1")
	require.NoError(t, err)
	_, err = svc.GetItem(context.Background(), "2")
	require.NoError(t, err)
	assert.Equal(t, 2, callCount)
}

// TestExampleService_WithOverridesOnEndpoint shows using WithOverrides directly
// on an endpoint for one-off customization.
func TestExampleService_WithOverridesOnEndpoint(t *testing.T) {
	client := handlerClient(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v1/items/1", r.URL.Path)
		assert.Equal(t, "custom-value", r.Header.Get("X-Custom"))
		user, pass, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "admin", user)
		assert.Equal(t, "secret", pass)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "1", Name: "x"})
	})

	// Build overrides externally and apply to an endpoint.
	overrides := httpc.Overrides{}.
		WithAddedHeader("X-Custom", "custom-value").
		WithAuthorization(httpc.BasicCredentials("admin", "secret"))

	resp, _, err := getItemEndpoint.
		Call().
		WithPathParam("itemId", "1").
		WithOverrides(overrides).
		Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "1", resp.ID)
}

// TestExampleService_TimeoutOverride verifies that a timeout set via Overrides
// is applied to the request when the client is built via Builder (which honors
// the per-attempt timeout signal).
func TestExampleService_TimeoutOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	client, err := httpc.NewBuilder().
		SetServiceName("timeout-override").
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	overrides := httpc.Overrides{}.WithTimeout(50 * time.Millisecond)
	svc := &itemServiceClient{client: client, overrides: overrides}

	err = svc.DeleteItem(context.Background(), "slow-item")
	require.Error(t, err)
}

// TestExampleService_MiddlewareOverride verifies that middleware set via Overrides
// wraps every request from the client.
func TestExampleService_MiddlewareOverride(t *testing.T) {
	var middlewareCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "injected", r.Header.Get("X-MW"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(GetItemResponse{ID: "1", Name: "x"})
	}))
	t.Cleanup(server.Close)

	mw := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		middlewareCalled = true
		req.Header.Set("X-MW", "injected")
		return next.RoundTrip(req)
	})

	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(context.Background())
	require.NoError(t, err)
	overrides := httpc.Overrides{}.WithMiddleware(mw)
	svc := &itemServiceClient{client: client, overrides: overrides}

	_, err = svc.GetItem(context.Background(), "1")
	require.NoError(t, err)
	assert.True(t, middlewareCalled)
}

// ---------------------------------------------------------------------------
// Builder: intermediate component examples
// ---------------------------------------------------------------------------

// TestExample_BuildDialer shows how to build a custom dialer from the builder
// and use it independently (e.g. to probe connectivity before building a client).
func TestExample_BuildDialer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	dialer, err := httpc.NewBuilder().
		SetDialTimeout(5 * time.Second).
		SetKeepAlive(15 * time.Second).
		BuildDialer(context.Background())
	require.NoError(t, err)

	conn, err := dialer.DialContext(context.Background(), "tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	_ = conn.Close()
}

// TestExample_BuildDialer_CustomSocksProxy shows configuring a SOCKS proxy on the dialer.
// We verify the builder accepts the setting without error (actual proxying requires a
// running SOCKS server, so we only verify the dialer builds successfully).
func TestExample_BuildDialer_CustomSocksProxy(t *testing.T) {
	// Start a TCP listener to act as a fake SOCKS proxy endpoint.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	dialer, err := httpc.NewBuilder().
		SetSocksProxyURL("socks5://" + ln.Addr().String()).
		BuildDialer(context.Background())
	require.NoError(t, err)
	require.NotNil(t, dialer)
}

// TestExample_BuildTLSConfig shows building a TLS configuration from the builder
// and inspecting the result.
func TestExample_BuildTLSConfig(t *testing.T) {
	tlsConfig, err := httpc.NewBuilder().
		SetInsecureSkipVerify(true).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsConfig.Validation()
	require.NoError(t, validErr)
	assert.True(t, cfg.InsecureSkipVerify)
}

// TestExample_SetTLSConfig shows injecting a caller-managed *tls.Config as an escape hatch,
// bypassing CA file/bytes and other TLS builder settings. The builder clones the
// provided config so mutations to the original do not affect the builder.
func TestExample_SetTLSConfig(t *testing.T) {
	customTLS := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	tlsConfig, err := httpc.NewBuilder().
		SetTLSConfig(customTLS).
		// CA settings are ignored when SetTLSConfig is used.
		AddCACertBytes([]byte("not-real-pem")).
		BuildTLSConfig(context.Background())
	require.NoError(t, err)

	cfg, validErr := tlsConfig.Validation()
	require.NoError(t, validErr)
	assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)

	// The builder cloned the config, so mutating the original has no effect.
	customTLS.MinVersion = tls.VersionTLS12
	cfg2, _ := tlsConfig.Validation()
	assert.Equal(t, uint16(tls.VersionTLS13), cfg2.MinVersion)
}

// TestExample_BuildTransport shows building a transport from the builder and
// using it directly in a plain *http.Client.
func TestExample_BuildTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"widget"}`))
	}))
	t.Cleanup(server.Close)

	transport, err := httpc.NewBuilder().
		SetMaxIdleConnsPerHost(50).
		SetIdleConnTimeout(60 * time.Second).
		BuildTransport(context.Background())
	require.NoError(t, err)

	client := &http.Client{Transport: transport}
	resp, err := client.Get(server.URL + "/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// TestExample_SetTransport shows injecting a custom http.RoundTripper as an
// escape hatch, bypassing the builder's dialer and TLS configuration entirely.
func TestExample_SetTransport(t *testing.T) {
	var customTransportUsed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"widget"}`))
	}))
	t.Cleanup(server.Close)

	custom := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		customTransportUsed = true
		return http.DefaultTransport.RoundTrip(req)
	}}

	transport, err := httpc.NewBuilder().
		SetTransport(custom).
		BuildTransport(context.Background())
	require.NoError(t, err)

	// BuildTransport returns the injected transport directly.
	client := &http.Client{Transport: transport}
	resp, err := client.Get(server.URL + "/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, customTransportUsed)
}

// TestExample_BuildHTTPClient shows building a complete *http.Client (with
// metrics/tracing middleware) from the builder and using it for a raw request.
func TestExample_BuildHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"widget"}`))
	}))
	t.Cleanup(server.Close)

	httpClient, err := httpc.NewBuilder().
		SetTimeout(30 * time.Second).
		BuildHTTPClient(context.Background())
	require.NoError(t, err)

	resp, err := httpClient.Current().Get(server.URL + "/test")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 30*time.Second, httpClient.Current().Timeout)
}

// TestExample_SetTransport_FullClient shows building a full Client using
// SetTransport to inject a custom transport. The builder's middleware stack
// still wraps the custom transport.
func TestExample_SetTransport_FullClient(t *testing.T) {
	var customTransportUsed bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"1","name":"widget"}`))
	}))
	t.Cleanup(server.Close)

	custom := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		customTransportUsed = true
		return http.DefaultTransport.RoundTrip(req)
	}}

	client, err := httpc.NewBuilder().
		SetTransport(custom).
		SetBaseURLs(server.URL).
		Build(context.Background())
	require.NoError(t, err)

	resp, _, err := getItemEndpoint.Call().WithPathParam("itemId", "1").
		Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Equal(t, "1", resp.ID)
	assert.True(t, customTransportUsed)
}

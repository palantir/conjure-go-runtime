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
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSendOptions_PublicValuesAndPolicy exercises the honest SendOptions contract:
// a caller using Send directly (no Endpoint) expresses header, query, and basic
// auth decoration through the public RequestValues, and the standard runtime
// resolves them onto the request — no unexported friend fields involved.
func TestSendOptions_PublicValuesAndPolicy(t *testing.T) {
	var gotAuth, gotTenant, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTenant = r.Header.Get("X-Tenant")
		gotQuery = r.URL.Query().Get("q")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(ctx)
	require.NoError(t, err)

	// Path-only request; the runtime prepends the selected base URL.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
	require.NoError(t, err)

	opts := httpc.SendOptions{
		Values: httpc.RequestValues{}.
			WithHeader("X-Tenant", "acme").
			WithAddedQuery("q", "v").
			WithAuthorization(httpc.BasicCredentials("user", "pass")),
	}
	resp, err := client.Send(ctx, req, opts)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "v", gotQuery)
	assert.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("user:pass")), gotAuth)
}

// TestSendRejectsNonRelativeURL pins the path-only contract: the standard runtime
// supplies scheme/host per attempt, so a non-relative request URL is rejected with
// ErrNonRelativeRequestURL rather than silently rewritten, while a plain relative
// path succeeds.
func TestSendRejectsNonRelativeURL(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(ctx)
	require.NoError(t, err)

	t.Run("relative path succeeds", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/items/widget?q=1", nil)
		require.NoError(t, err)
		resp, err := client.Send(ctx, req, httpc.SendOptions{})
		require.NoError(t, err)
		_ = resp.Body.Close()
	})

	for name, rawURL := range map[string]string{
		"absolute":        "http://other.example.com/p",
		"scheme-relative": "//other.example.com/p",
		"opaque":          "mailto:ops@example.com",
	} {
		t.Run(name+" is rejected", func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			require.NoError(t, err)
			_, err = client.Send(ctx, req, httpc.SendOptions{})
			require.Error(t, err)
			assert.True(t, errors.As(err, &httpc.ErrNonRelativeRequestURL{}), "want ErrNonRelativeRequestURL, got %v", err)
		})
	}

	t.Run("nil URL is rejected", func(t *testing.T) {
		_, err := client.Send(ctx, &http.Request{}, httpc.SendOptions{})
		require.Error(t, err)
	})
	t.Run("nil request is rejected", func(t *testing.T) {
		_, err := client.Send(ctx, nil, httpc.SendOptions{})
		require.Error(t, err)
	})
}

// TestSend_DirectRequestHeaderBeatsBuilderDecoration pins the precedence for a
// caller using Send directly: headers set on the *http.Request are hoisted above
// the builder-intrinsic layer, so they beat builder auth/headers and suppress an
// overridden auth provider — the same contract Call.Execute provides.
func TestSend_DirectRequestHeaderBeatsBuilderDecoration(t *testing.T) {
	ctx := context.Background()

	t.Run("request Authorization beats builder auth token", func(t *testing.T) {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)
		client, err := httpc.NewBuilder().SetBaseURLs(server.URL).SetAuthToken("builder-token").Build(ctx)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer caller")

		resp, err := client.Send(ctx, req, httpc.SendOptions{})
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, "Bearer caller", got)
	})

	t.Run("request Authorization suppresses the builder auth provider", func(t *testing.T) {
		providerCalled := false
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)
		client, err := httpc.NewBuilder().SetBaseURLs(server.URL).
			SetAuth(httpc.BearerTokenProvider(func(context.Context) (string, error) {
				providerCalled = true
				return "provider-token", nil
			})).
			Build(ctx)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer caller")

		resp, err := client.Send(ctx, req, httpc.SendOptions{})
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, "Bearer caller", got)
		assert.False(t, providerCalled, "the builder auth provider must not run when the request sets Authorization")
	})

	t.Run("request header beats builder SetHeader", func(t *testing.T) {
		var got string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = r.Header.Get("X-Env")
			w.WriteHeader(http.StatusNoContent)
		}))
		t.Cleanup(server.Close)
		client, err := httpc.NewBuilder().SetBaseURLs(server.URL).SetHeader("X-Env", "builder").Build(ctx)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/x", nil)
		require.NoError(t, err)
		req.Header.Set("X-Env", "caller")

		resp, err := client.Send(ctx, req, httpc.SendOptions{})
		require.NoError(t, err)
		_ = resp.Body.Close()
		assert.Equal(t, "caller", got)
	})
}

type widgetItem struct {
	Name string `json:"name"`
}

// captureRuntime is an external one-method httpc.Runtime: it implements only Send,
// capturing what it receives and returning a canned response.
type captureRuntime struct {
	gotReq  *http.Request
	gotOpts httpc.SendOptions
	resp    *http.Response
}

func (c *captureRuntime) Send(_ context.Context, req *http.Request, opts httpc.SendOptions) (*http.Response, error) {
	c.gotReq, c.gotOpts = req, opts
	return c.resp, nil
}

// TestExecute_OneMethodRuntimeFake proves a fake implementing only Send works
// with Call.Execute: Execute builds the path-only request + SendOptions, drives
// the runtime, then decodes the response.
func TestExecute_OneMethodRuntimeFake(t *testing.T) {
	fake := &captureRuntime{
		resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"name":"widget"}`)),
		},
	}

	ep := httpc.NewGET[widgetItem]("GetItem", "/items/widget").WithJSON().
		WithHeader("X-Tenant", "acme")

	out, resp, err := ep.Call().Execute(context.Background(), fake)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "widget", out.Name)

	require.NotNil(t, fake.gotReq)
	assert.Equal(t, http.MethodGet, fake.gotReq.Method)
	assert.Equal(t, "/items/widget", fake.gotReq.URL.Path)
	assert.Empty(t, fake.gotReq.Header, "decoration travels in opts.Values, not pre-written on req")

	// Snapshot lets a one-method fake inspect the decoration it received — the
	// endpoint header and the JSON Accept are both carried in opts.Values.
	header, _, err := fake.gotOpts.Values.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "acme", header.Get("X-Tenant"))
	assert.Equal(t, "application/json", header.Get("Accept"))
}

// TestEndpoint_CodecHeadersBeatBuilderHeaders covers review finding #1: a
// client-level SetHeader must not override the endpoint's codec headers (Accept
// from WithAccept, Content-Type from the encoder), which now flow through
// SendOptions.Values above the builder-intrinsic layer.
func TestEndpoint_CodecHeadersBeatBuilderHeaders(t *testing.T) {
	var gotAccept, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"ok"}`)
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	client, err := httpc.NewBuilder().
		SetBaseURLs(server.URL).
		SetHeader("Accept", "wrong/accept").
		SetHeader("Content-Type", "wrong/content-type").
		Build(ctx)
	require.NoError(t, err)

	ep := httpc.NewPOST[widgetItem, widgetItem]("Create", "/items").WithJSON()
	_, _, err = ep.Call(widgetItem{Name: "x"}).Execute(ctx, client)
	require.NoError(t, err)

	assert.Equal(t, "application/json", gotAccept, "endpoint WithAccept beats builder SetHeader(Accept)")
	assert.Equal(t, "application/json", gotContentType, "JSON encoder Content-Type beats builder SetHeader(Content-Type)")
}

// TestEndpoint_PerCallHeaderBeatsEndpointAccept covers the upper end of the
// precedence: a per-call WithHeader still wins over the endpoint's WithAccept.
func TestEndpoint_PerCallHeaderBeatsEndpointAccept(t *testing.T) {
	var gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	ctx := context.Background()
	client, err := httpc.NewBuilder().SetBaseURLs(server.URL).Build(ctx)
	require.NoError(t, err)

	ep := httpc.NewGET[struct{}]("Get", "/x").
		WithDecoder(httpc.VoidDecoder()).
		WithAccept("application/json")
	_, _, err = ep.Call().WithHeader("Accept", "text/plain").Execute(ctx, client)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", gotAccept, "per-call WithHeader(Accept) beats endpoint WithAccept")
}

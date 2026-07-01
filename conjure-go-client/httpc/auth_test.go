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
	"sync/atomic"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sendCapturingAuth builds a client with a transport that records the Authorization
// header, optionally configures the builder and the per-call request, executes one
// GET, and returns what reached the wire. configure and perCall may be nil.
func sendCapturingAuth(t *testing.T, configure func(b *httpc.Builder), perCall func(c httpc.Call[struct{}]) httpc.Call[struct{}]) (string, error) {
	t.Helper()
	var got string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("Authorization")
		return emptyResponse(req), nil
	}}
	b := httpc.NewBuilder().SetBaseURLs("https://example.com").SetTransport(transport)
	if configure != nil {
		configure(b)
	}
	client, err := b.Build(context.Background())
	require.NoError(t, err)

	c := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").WithDecoder(httpc.VoidDecoder()).Call()
	if perCall != nil {
		c = perCall(c)
	}
	_, _, err = c.Execute(context.Background(), client)
	return got, err
}

// TestAuth_ConstructorParity pins what each Authorizer constructor puts on the wire
// when installed via SetAuth — the dropped setters lost their only call sites, so
// this is the behavior-parity net for the constructors that replaced them.
func TestAuth_ConstructorParity(t *testing.T) {
	bearerTok := "bt"
	basicCreds := &httpc.BasicAuth{User: "u", Password: "p"}
	for _, tc := range []struct {
		name string
		auth httpc.Authorizer
		want string
	}{
		{"BearerToken", httpc.BearerToken("bt"), "Bearer bt"},
		{"BearerTokenProvider", httpc.BearerTokenProvider(func(context.Context) (string, error) { return "bt", nil }), "Bearer bt"},
		{"RefreshableBearerToken", httpc.RefreshableBearerToken(refreshable.New(&bearerTok)), "Bearer bt"},
		{"BasicCredentials", httpc.BasicCredentials("u", "p"), "Basic dTpw"},
		{"BasicCredentialsProvider", httpc.BasicCredentialsProvider(func(context.Context) (httpc.BasicAuth, error) {
			return httpc.BasicAuth{User: "u", Password: "p"}, nil
		}), "Basic dTpw"},
		{"OptionalBasicCredentials", httpc.OptionalBasicCredentials(func(context.Context) (*httpc.BasicAuth, error) {
			return &httpc.BasicAuth{User: "u", Password: "p"}, nil
		}), "Basic dTpw"},
		{"RefreshableBasicCredentials", httpc.RefreshableBasicCredentials(refreshable.New(basicCreds)), "Basic dTpw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sendCapturingAuth(t, func(b *httpc.Builder) { b.SetAuth(tc.auth) }, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestAuth_RefreshableDisablesOnNil verifies the refreshable constructors honor a
// live switch to a nil value: the header is set while a value is present and unset
// once it goes nil, resolved per attempt off the same client.
func TestAuth_RefreshableDisablesOnNil(t *testing.T) {
	tok := "first"
	tokR := refreshable.New(&tok)
	bcR := refreshable.New(&httpc.BasicAuth{User: "u", Password: "p"})

	for _, tc := range []struct {
		name    string
		auth    httpc.Authorizer
		want    string
		disable func()
	}{
		{"bearer", httpc.RefreshableBearerToken(tokR), "Bearer first", func() { tokR.Update(nil) }},
		{"basic", httpc.RefreshableBasicCredentials(bcR), "Basic dTpw", func() { bcR.Update(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
				got = req.Header.Get("Authorization")
				return emptyResponse(req), nil
			}}
			client, err := httpc.NewBuilder().
				SetBaseURLs("https://example.com").
				SetTransport(transport).
				SetAuth(tc.auth).
				Build(context.Background())
			require.NoError(t, err)
			ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").WithDecoder(httpc.VoidDecoder())

			_, _, err = ep.Call().Execute(context.Background(), client)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got, "value present → header set")

			tc.disable()
			_, _, err = ep.Call().Execute(context.Background(), client)
			require.NoError(t, err)
			assert.Empty(t, got, "nil current value → header unset")
		})
	}
}

// TestAuth_PerCallBearerBeatsBuilder covers the net-new per-call bearer path: a
// per-call WithAuthorization(BearerToken) wins over the builder's authorizer.
func TestAuth_PerCallBearerBeatsBuilder(t *testing.T) {
	got, err := sendCapturingAuth(t,
		func(b *httpc.Builder) { b.SetAuth(httpc.BearerToken("builder")) },
		func(c httpc.Call[struct{}]) httpc.Call[struct{}] {
			return c.WithAuthorization(httpc.BearerToken("call"))
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "Bearer call", got)
}

// TestAuth_NoAuthorizationSuppressesWithoutInvoking proves NoAuthorization is a
// present, winning contributor: it leaves Authorization unset and the suppressed
// builder provider is never invoked (no fall-through).
func TestAuth_NoAuthorizationSuppressesWithoutInvoking(t *testing.T) {
	var providerCalled atomic.Bool
	got, err := sendCapturingAuth(t,
		func(b *httpc.Builder) {
			b.SetAuth(httpc.BearerTokenProvider(func(context.Context) (string, error) {
				providerCalled.Store(true)
				return "builder-token", nil
			}))
		},
		func(c httpc.Call[struct{}]) httpc.Call[struct{}] { return c.WithAuthorization(httpc.NoAuthorization()) },
	)
	require.NoError(t, err)
	assert.Empty(t, got, "NoAuthorization sends no Authorization header")
	assert.False(t, providerCalled.Load(), "the suppressed builder provider must never run")
}

// TestAuth_SetAuthNilClears verifies a nil argument to SetAuth clears a previously
// installed authorizer (the slot's default/unset state).
func TestAuth_SetAuthNilClears(t *testing.T) {
	got, err := sendCapturingAuth(t, func(b *httpc.Builder) {
		b.SetAuth(httpc.BearerToken("x"))
		b.SetAuth(nil)
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestAuth_NilProviderErrorsAtResolution covers D5: a constructor handed a nil
// provider, and a directly-constructed nil AuthorizerFunc, both error at resolution
// rather than panicking.
func TestAuth_NilProviderErrorsAtResolution(t *testing.T) {
	_, err := sendCapturingAuth(t, func(b *httpc.Builder) {
		b.SetAuth(httpc.BearerTokenProvider(nil))
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nil provider passed to auth constructor")

	_, ferr := httpc.AuthorizerFunc(nil).AuthorizationHeader(context.Background())
	require.Error(t, ferr)
	assert.Contains(t, ferr.Error(), "nil AuthorizerFunc")
}

// TestAuth_ApplyConfigRefreshableSwitching guards the combined-closure rewrite: a
// refreshable config that flips api-token → basic-auth → neither resolves correctly
// and falls back to a prior SetAuth when the config specifies neither.
func TestAuth_ApplyConfigRefreshableSwitching(t *testing.T) {
	var got string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("Authorization")
		return emptyResponse(req), nil
	}}

	cfg := httpc.ClientConfig{ServiceName: "svc", URIs: []string{"https://example.com"}, APIToken: new("tok1")}
	cfgR := refreshable.New(cfg)

	b := httpc.NewBuilder().SetTransport(transport).SetAuth(httpc.BearerToken("fallback"))
	b.ApplyConfigRefreshable(context.Background(), cfgR)
	client, err := b.Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").WithDecoder(httpc.VoidDecoder())
	exec := func() string {
		_, _, e := ep.Call().Execute(context.Background(), client)
		require.NoError(t, e)
		return got
	}

	assert.Equal(t, "Bearer tok1", exec(), "config api-token applies")

	cfg.APIToken = nil
	cfg.BasicAuth = &httpc.BasicAuth{User: "u", Password: "p"}
	cfgR.Update(cfg)
	assert.Equal(t, "Basic dTpw", exec(), "switches to config basic auth")

	cfg.BasicAuth = nil
	cfgR.Update(cfg)
	assert.Equal(t, "Bearer fallback", exec(), "neither set → falls back to the prior SetAuth")
}

// Overrides.WithAuthorization wins over an Overrides.WithHeader("Authorization", ...).
func TestEndpointExecute_AuthorizerOverridesAuthorizationHeader(t *testing.T) {
	var seen string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("Authorization")
		return emptyResponse(req), nil
	}}
	client, err := httpc.NewBuilder().SetBaseURLs("https://example.com").SetTransport(transport).Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "Auth", "/auth").
		WithDecoder(httpc.VoidDecoder()).
		WithHeader("Authorization", "Bearer ignored").
		WithAuthorization(httpc.BasicCredentials("u", "p"))

	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	// The authorizer is the trailing Authorization contributor, so it wins.
	assert.Equal(t, "Basic dTpw", seen)
}

// OptionalBasicCredentials returning nil leaves the Authorization header unset.
func TestBuilder_OptionalBasicCredentials_NilSkipsAuth(t *testing.T) {
	var seen string
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("Authorization")
		return emptyResponse(req), nil
	}}
	client, err := httpc.NewBuilder().
		SetBaseURLs("https://example.com").
		SetTransport(transport).
		SetAuth(httpc.OptionalBasicCredentials(func(context.Context) (*httpc.BasicAuth, error) { return nil, nil })).
		Build(context.Background())
	require.NoError(t, err)

	ep := httpc.NewNoBodyEndpoint[struct{}](http.MethodGet, "T", "/t").WithDecoder(httpc.VoidDecoder())
	_, _, err = ep.Call().Execute(context.Background(), client)
	require.NoError(t, err)
	assert.Empty(t, seen, "nil from OptionalBasicCredentials should leave Authorization unset")
}

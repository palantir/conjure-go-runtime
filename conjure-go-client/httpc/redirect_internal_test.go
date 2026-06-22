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

package httpc

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// authHeaderAllowedOnRedirect allows credentials only while a redirect chain stays
// on the original host or a subdomain of it — matching net/http's own stripping.
func TestAuthHeaderAllowedOnRedirect(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain []string
		allow bool
	}{
		{name: "initial request", chain: []string{"https://a.com/"}, allow: true},
		{name: "same host redirect", chain: []string{"https://a.com/", "https://a.com/other"}, allow: true},
		{name: "subdomain redirect", chain: []string{"https://a.com/", "https://sub.a.com/"}, allow: true},
		{name: "cross host redirect", chain: []string{"https://a.com/", "https://b.com/"}, allow: false},
		{name: "cross host then subdomain of intermediate", chain: []string{"https://a.com/", "https://b.com/", "https://sub.b.com/"}, allow: false},
		{name: "subdomain then original", chain: []string{"https://a.com/", "https://sub.a.com/", "https://a.com/"}, allow: true},
		{name: "cross host then return to original", chain: []string{"https://a.com/", "https://b.com/", "https://a.com/"}, allow: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allow, authHeaderAllowedOnRedirect(redirectChain(t, tc.chain)))
		})
	}
}

// relocationAllowed confines a 307/308 relocation to a configured target on scheme, host,
// effective port, and base-path prefix. originAuthorized gates the sensitive headers on
// the target's origin only (scheme+host+port), so a subdomain — a configured target's
// subdomain is not itself a configured target — is refused by both.
func TestConfiguredTargetGates(t *testing.T) {
	targets := configuredTargetsFromURIs([]string{"https://a.example.com/svc", "mesh-https://b.example.com:8443"})

	t.Run("relocation", func(t *testing.T) {
		for _, tc := range []struct {
			uri   string
			allow bool
		}{
			{uri: "https://a.example.com/svc", allow: true},
			{uri: "https://a.example.com/svc/items", allow: true},  // under the base path
			{uri: "https://a.example.com:443/svc", allow: true},    // explicit default port
			{uri: "https://a.example.com/other", allow: false},     // same origin, wrong base path
			{uri: "https://a.example.com/svc-2", allow: false},     // segment boundary, not a prefix
			{uri: "http://a.example.com/svc", allow: false},        // scheme downgrade
			{uri: "https://node1.a.example.com/svc", allow: false}, // subdomain is not configured
			{uri: "https://evil.example.com/svc", allow: false},
			{uri: "https://b.example.com:8443/x", allow: true}, // mesh target, de-meshed scheme + explicit port
			{uri: "https://b.example.com/x", allow: false},     // wrong port (443 vs 8443)
		} {
			t.Run(tc.uri, func(t *testing.T) {
				assert.Equal(t, tc.allow, relocationAllowed(tc.uri, targets))
			})
		}
		assert.False(t, relocationAllowed("https://a.example.com/svc", nil), "no targets allows no relocation")
	})

	t.Run("origin", func(t *testing.T) {
		mustParse := func(s string) *url.URL {
			u, err := url.Parse(s)
			require.NoError(t, err)
			return u
		}
		for _, tc := range []struct {
			uri   string
			allow bool
		}{
			{uri: "https://a.example.com/anything", allow: true}, // origin match, base path ignored for auth
			{uri: "https://a.example.com:443/x", allow: true},
			{uri: "https://node1.a.example.com/svc", allow: false}, // subdomain is a different origin
			{uri: "http://a.example.com/svc", allow: false},
			{uri: "https://b.example.com:8443/x", allow: true},
		} {
			t.Run(tc.uri, func(t *testing.T) {
				assert.Equal(t, tc.allow, originAuthorized(mustParse(tc.uri), targets))
			})
		}
		assert.False(t, originAuthorized(mustParse("https://a.example.com/svc"), nil), "no targets authorizes nothing")
	})
}

// The cross-host sensitive-header strip matches the full net/http set and is
// case-insensitive, so a raw non-canonical map-write is dropped as a contributor and
// physically deleted alongside a canonical one.
func TestRedirectSensitiveHeaderStrip(t *testing.T) {
	assert.True(t, isRedirectSensitiveHeader("authorization"))
	assert.True(t, isRedirectSensitiveHeader("Cookie"))
	assert.True(t, isRedirectSensitiveHeader("cookie2"))
	assert.True(t, isRedirectSensitiveHeader("proxy-authorization"))
	assert.True(t, isRedirectSensitiveHeader("Www-Authenticate"))
	assert.False(t, isRedirectSensitiveHeader("X-Tenant"))

	vals := []requestValue[http.Header]{
		setValue[http.Header]{name: "authorization", values: []string{"Bearer x"}}, // non-canonical
		setValue[http.Header]{name: "Cookie", values: []string{"s=1"}},
		setValue[http.Header]{name: "X-Tenant", values: []string{"acme"}},
	}
	kept := withoutRedirectSensitiveHeaders(vals)
	require.Len(t, kept, 1)
	assert.Equal(t, "X-Tenant", kept[0].key())

	h := http.Header{"authorization": {"Bearer x"}, "Cookie": {"s=1"}, "X-Tenant": {"acme"}}
	deleteRedirectSensitiveHeaders(h)
	assert.NotContains(t, h, "authorization")
	assert.NotContains(t, h, "Cookie")
	assert.Equal(t, "acme", h.Get("X-Tenant"))
}

// redirectChain builds a request whose Response chain models following each URL in order.
func redirectChain(t *testing.T, urls []string) *http.Request {
	var prev *http.Request
	for _, singleURL := range urls {
		u, err := url.Parse(singleURL)
		require.NoError(t, err)
		req := &http.Request{URL: u, Header: make(http.Header)}
		if prev != nil {
			req.Response = &http.Response{Request: prev}
		}
		prev = req
	}
	return prev
}

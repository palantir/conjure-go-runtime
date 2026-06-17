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

package httpclient

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthHeaderAllowedOnRedirect exercises the redirect host comparison across multi-hop chains.
// Each test case lists the hosts of a redirect chain ordered from the original request to the
// current target; the chain is wired the way net/http links requests while following redirects.
func TestAuthHeaderAllowedOnRedirect(t *testing.T) {
	for _, tc := range []struct {
		name  string
		chain []string
		allow bool
	}{
		{
			name:  "initial request",
			chain: []string{"https://a.com/"},
			allow: true,
		},
		{
			name:  "same host redirect",
			chain: []string{"https://a.com/", "https://a.com/other"},
			allow: true,
		},
		{
			name:  "subdomain redirect",
			chain: []string{"https://a.com/", "https://sub.a.com/"},
			allow: true,
		},
		{
			name:  "cross host redirect",
			chain: []string{"https://a.com/", "https://b.com/"},
			allow: false,
		},
		{
			// Without comparing against the original host, the hop to sub.b.com would be treated as a
			// subdomain of the previous hop b.com and a.com's credentials would leak to sub.b.com.
			name:  "cross host then subdomain of intermediate",
			chain: []string{"https://a.com/", "https://b.com/", "https://sub.b.com/"},
			allow: false,
		},
		{
			// All hops stay within the original domain, so credentials are preserved.
			name:  "subdomain then original",
			chain: []string{"https://a.com/", "https://sub.a.com/", "https://a.com/"},
			allow: true,
		},
		{
			// Leaving the original host strips credentials for the remainder of the chain, even when a
			// later hop returns to the original host.
			name:  "leave then return to original",
			chain: []string{"https://a.com/", "https://b.com/", "https://a.com/"},
			allow: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allow, authHeaderAllowedOnRedirect(redirectChain(t, tc.chain)))
		})
	}
}

// redirectChain builds the *http.Request that net/http would dispatch for the final host in hosts,
// linking each hop to the prior one via req.Response.Request exactly as the standard library does
// while following redirects. The first entry is the original request (its Response is left nil).
func redirectChain(t *testing.T, rawURLs []string) *http.Request {
	var prev *http.Request
	for _, raw := range rawURLs {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		req := &http.Request{URL: u, Header: make(http.Header)}
		if prev != nil {
			req.Response = &http.Response{Request: prev}
		}
		prev = req
	}
	return prev
}

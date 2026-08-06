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

// Verifies the redirect host comparison across multiple hops.
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
			allow: false,
		},
		{
			name:  "default port redirect",
			chain: []string{"https://a.com/", "https://a.com:443/"},
			allow: true,
		},
		{
			name:  "cross port redirect",
			chain: []string{"https://a.com:8443/", "https://a.com:9443/"},
			allow: false,
		},
		{
			name:  "scheme downgrade redirect",
			chain: []string{"https://a.com/", "http://a.com/"},
			allow: false,
		},
		{
			name:  "cross host redirect",
			chain: []string{"https://a.com/", "https://b.com/"},
			allow: false,
		},
		{
			name:  "cross host then subdomain of intermediate",
			chain: []string{"https://a.com/", "https://b.com/", "https://sub.b.com/"},
			allow: false,
		},
		{
			name:  "subdomain then original",
			chain: []string{"https://a.com/", "https://sub.a.com/", "https://a.com/"},
			allow: false,
		},
		{
			name:  "cross host then return to original",
			chain: []string{"https://a.com/", "https://b.com/", "https://a.com/"},
			allow: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.allow, authHeaderAllowedOnRedirect(redirectChain(t, tc.chain)))
		})
	}
}

func TestRedirectSensitiveHeaders(t *testing.T) {
	for _, key := range []string{
		"Authorization",
		"Www-Authenticate",
		"Cookie",
		"Cookie2",
		"Proxy-Authorization",
		"Proxy-Authenticate",
	} {
		_, ok := redirectSensitiveHeaders[http.CanonicalHeaderKey(key)]
		assert.True(t, ok, key)
	}
	_, ok := redirectSensitiveHeaders[http.CanonicalHeaderKey("X-Test")]
	assert.False(t, ok)
}

func redirectChain(t *testing.T, urls []string) *http.Request {
	var prev *http.Request

	for _, singleURL := range urls {
		u, err := url.Parse(singleURL)
		require.NoError(t, err)
		req := &http.Request{
			URL:    u,
			Header: make(http.Header),
		}
		if prev != nil {
			req.Response = &http.Response{
				Request: prev,
			}
		}
		prev = req
	}
	return prev
}

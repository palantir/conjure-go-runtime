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
	"encoding/base64"
	"net/http"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// redirectingTransport answers the initial "/" request to the origin host with a redirect
// (status, default 302) to redirectTo, and every other request with 200, recording the
// Authorization header seen at each host. Gating the redirect on the initial path avoids a loop
// when redirectTo is itself on the origin host (the same-host case).
type redirectingTransport struct {
	originHost string
	redirectTo string
	status     int
	authByHost map[string]string
}

func (t *redirectingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.authByHost[req.URL.Host] = req.Header.Get("Authorization")
	if req.URL.Host == t.originHost && req.URL.Path == "/" {
		status := t.status
		if status == 0 {
			status = http.StatusFound
		}
		return &http.Response{
			StatusCode: status,
			Header:     http.Header{"Location": []string{t.redirectTo}},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
}

// A 302 to a different host must not re-attach Authorization on the redirect hop:
// the stdlib strips it cross-host and decoration must not add it back. Covers builder
// bearer, builder basic, and a raw per-call Authorization header (the whole key is gated,
// not just the SetAuth authorizer).
func TestSend_AuthNotLeakedOnCrossHostRedirect(t *testing.T) {
	const originHost = "origin.example.com"
	const targetHost = "evil.example.com"
	basicHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))

	for _, tc := range []struct {
		name         string
		configure    func(*httpc.Builder)
		perCall      func(httpc.Call[struct{}]) httpc.Call[struct{}]
		expectOrigin string
	}{
		{
			name:         "builder bearer",
			configure:    func(b *httpc.Builder) { b.SetAuth(httpc.BearerToken("tok")) },
			expectOrigin: "Bearer tok",
		},
		{
			name:         "builder basic",
			configure:    func(b *httpc.Builder) { b.SetBasicAuth("user", "pass") },
			expectOrigin: basicHeader,
		},
		{
			name:         "raw per-call header",
			configure:    func(*httpc.Builder) {},
			perCall:      func(c httpc.Call[struct{}]) httpc.Call[struct{}] { return c.WithHeader("Authorization", "Bearer raw") },
			expectOrigin: "Bearer raw",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &redirectingTransport{
				originHost: originHost,
				redirectTo: "https://" + targetHost + "/",
				authByHost: map[string]string{},
			}

			b := httpc.NewBuilder().
				SetServiceName("redirect-auth").
				SetBaseURLs("https://" + originHost).
				SetTransport(transport)
			tc.configure(b)
			client, err := b.Build(t.Context())
			require.NoError(t, err)

			call := httpc.NewGET[struct{}]("Redirect", "/").WithDecoder(httpc.VoidDecoder()).Call()
			if tc.perCall != nil {
				call = tc.perCall(call)
			}
			_, _, err = call.Execute(t.Context(), client)
			require.NoError(t, err)

			assert.Equal(t, tc.expectOrigin, transport.authByHost[originHost], "origin must receive the credential")
			assert.Empty(t, transport.authByHost[targetHost], "credential must not leak across the cross-host redirect")
		})
	}
}

// A same-host redirect keeps Authorization: the stdlib preserves it and decoration re-attaches it.
func TestSend_AuthPreservedOnSameHostRedirect(t *testing.T) {
	const originHost = "origin.example.com"
	transport := &redirectingTransport{
		originHost: originHost,
		redirectTo: "https://" + originHost + "/other",
		authByHost: map[string]string{},
	}

	client, err := httpc.NewBuilder().
		SetServiceName("redirect-auth-same-host").
		SetBaseURLs("https://" + originHost).
		SetAuth(httpc.BearerToken("tok")).
		SetTransport(transport).
		Build(t.Context())
	require.NoError(t, err)

	// Both hops share the origin host and write the same map entry; the last write is the
	// redirect hop, so the recorded value reflects whether auth survived the redirect.
	_, _, err = httpc.NewGET[struct{}]("Redirect", "/").WithDecoder(httpc.VoidDecoder()).Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", transport.authByHost[originHost], "auth is preserved on a same-host redirect")
}

// A 307/308 QoS relocation (handled by the retrier, not the http.Client) carries Authorization
// only when its Location host is a configured base-URL host or a subdomain of one. The retrier
// builds a fresh request to the Location, so this is gated by the configured-host allowlist
// rather than the redirect chain walk (which #80 uses for 301/302/303). These two cases use a
// single configured base URL so the origin is deterministic.
func TestSend_AuthOnQoSRelocation(t *testing.T) {
	const originHost = "a.example.com"
	for _, tc := range []struct {
		name        string
		relocateTo  string
		targetHost  string
		expectAuth  bool
		description string
	}{
		{
			name:        "foreign host drops auth",
			relocateTo:  "https://evil.example.com/",
			targetHost:  "evil.example.com",
			expectAuth:  false,
			description: "a relocation to an unconfigured host must not carry the credential",
		},
		{
			name:        "subdomain of configured node keeps auth",
			relocateTo:  "https://node1.a.example.com/",
			targetHost:  "node1.a.example.com",
			expectAuth:  true,
			description: "relocation to a subdomain of a configured node keeps the credential",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &redirectingTransport{
				originHost: originHost,
				redirectTo: tc.relocateTo,
				status:     http.StatusTemporaryRedirect, // 307: handled by the retrier, not http.Client
				authByHost: map[string]string{},
			}

			client, err := httpc.NewBuilder().
				SetServiceName("qos-relocation-auth").
				SetBaseURLs("https://" + originHost).
				SetAuth(httpc.BearerToken("tok")).
				SetTransport(transport).
				Build(t.Context())
			require.NoError(t, err)

			_, _, err = httpc.NewGET[struct{}]("Relocate", "/").WithDecoder(httpc.VoidDecoder()).Call().Execute(t.Context(), client)
			require.NoError(t, err)

			assert.Equal(t, "Bearer tok", transport.authByHost[originHost], "origin must receive the credential")
			if tc.expectAuth {
				assert.Equal(t, "Bearer tok", transport.authByHost[tc.targetHost], tc.description)
			} else {
				assert.Empty(t, transport.authByHost[tc.targetHost], tc.description)
			}
		})
	}
}

// A 307 relocation from one configured node to another keeps Authorization — the chosen policy
// trusts the whole configured node set. The transport relocates the first attempt to whichever
// configured node it did not land on (the URL selector shuffles equal-scored URLs), so both
// nodes end up authorized regardless of selection order.
func TestSend_AuthOnQoSRelocation_BetweenConfiguredNodes(t *testing.T) {
	const hostA, hostB = "a.example.com", "b.example.com"
	authByHost := map[string]string{}
	var redirected bool
	transport := &roundTripFunc{fn: func(req *http.Request) (*http.Response, error) {
		authByHost[req.URL.Host] = req.Header.Get("Authorization")
		if !redirected {
			redirected = true
			other := hostB
			if req.URL.Host == hostB {
				other = hostA
			}
			return &http.Response{
				StatusCode: http.StatusTemporaryRedirect,
				Header:     http.Header{"Location": []string{"https://" + other + "/"}},
				Body:       http.NoBody,
				Request:    req,
			}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: req}, nil
	}}

	client, err := httpc.NewBuilder().
		SetServiceName("qos-relocation-nodes").
		SetBaseURLs("https://"+hostA, "https://"+hostB).
		SetAuth(httpc.BearerToken("tok")).
		SetTransport(transport).
		Build(t.Context())
	require.NoError(t, err)

	_, _, err = httpc.NewGET[struct{}]("Relocate", "/").WithDecoder(httpc.VoidDecoder()).Call().Execute(t.Context(), client)
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", authByHost[hostA], "configured node keeps the credential")
	assert.Equal(t, "Bearer tok", authByHost[hostB], "configured node keeps the credential")
}

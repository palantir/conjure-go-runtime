// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"

	"github.com/palantir/pkg/refreshable/v2"
	"golang.org/x/net/idna"
)

// TokenProvider accepts a context and returns either:
//
// (1) a nonempty token and a nil error, or
//
// (2) an empty string and a non-nil error.
//
// A good implementation will request and cache an ephemeral client token.
type TokenProvider func(context.Context) (string, error)

type authTokenMiddleware struct {
	provideToken TokenProvider
}

// RoundTrip wraps an existing round tripper with a token providing round tripper.
// It sets the Authorization header using a newly provided token for each request.
func (h *authTokenMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	token, err := h.provideToken(req.Context())
	if err != nil {
		return nil, err
	}
	if token != "" {
		setAuthorizationHeader(req, "Bearer "+token)
	}
	return next.RoundTrip(req)
}

func newAuthTokenMiddlewareFromRefreshable(token refreshable.Refreshable[*string]) Middleware {
	return &authTokenMiddleware{
		provideToken: func(ctx context.Context) (string, error) {
			if s := token.Current(); s != nil {
				return *s, nil
			}
			return "", nil
		},
	}
}

// BasicAuthProvider accepts a context and returns either:
//
// (1) a nonempty BasicAuth and a nil error, or
//
// (2) an empty BasicAuth and a non-nil error.
type BasicAuthProvider func(context.Context) (BasicAuth, error)

// BasicAuthOptionalProvider accepts a context and returns either:
//
// (1) nil, nil to indicate that no BasicAuth should not be set on the request, or
//
// (2) a nonempty BasicAuth and a nil error, or
//
// (3) a nil BasicAuth and a non-nil error.
type BasicAuthOptionalProvider func(context.Context) (*BasicAuth, error)

func newBasicAuthMiddlewareFromRefreshable(auth refreshable.Refreshable[*BasicAuth]) Middleware {
	return MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		if basicAuth := auth.Current(); basicAuth != nil {
			setBasicAuth(req, basicAuth.User, basicAuth.Password)
		}
		return next.RoundTrip(req)
	})
}

func setBasicAuth(req *http.Request, username, password string) {
	setAuthorizationHeader(req, basicAuthValue(username, password))
}

func basicAuthValue(username, password string) string {
	basicAuthBytes := []byte(username + ":" + password)
	return "Basic " + base64.StdEncoding.EncodeToString(basicAuthBytes)
}

// sets the Authorization header on req to value, unless req targets a host that differs
// from the host that issued the request.
func setAuthorizationHeader(req *http.Request, value string) {
	if !authHeaderAllowedOnRedirect(req) {
		return
	}
	req.Header.Set("Authorization", value)
}

// authHeaderAllowedOnRedirect reports whether Authorization credentials may be attached to req.
//
// net/http populates req.Response only while following redirects: it points to the response that
// triggered this request, and req.Response.Request is the request that received that response (the
// previous hop). Walking back through this chain reaches the original request, whose Response is nil.
//
// To match net/http's redirect header-stripping semantics, credentials are allowed only when every
// redirect hop, including the current target, is the same as (or a subdomain of) the host of the
// ORIGINAL request. net/http compares each hop against the original request host and, once a
// cross-host hop occurs, strips sensitive headers for the remainder of the chain (see the sticky
// stripSensitiveHeaders logic in client.go). Comparing only against the immediately preceding hop
// would re-attach credentials on a chain such as a.com -> b.com -> sub.b.com, leaking a.com's
// credentials to sub.b.com.
//
// Source: https://github.com/golang/go/blob/go1.26.4/src/net/http/client.go#L688-L692
func authHeaderAllowedOnRedirect(req *http.Request) bool {
	if req.Response == nil {
		// Initial request, not a redirect, credentials are always allowed.
		return true
	}
	origin := originRequest(req)
	if origin.URL == nil {
		return true
	}
	originHost := idnaASCIIFromURL(origin.URL)
	// Require every redirect hop (from the hop after the original request through req) to remain on
	// the same host as, or a subdomain of, the original request host.
	for r := req; r != origin; {
		if r.URL == nil || !isDomainOrSubdomain(idnaASCIIFromURL(r.URL), originHost) {
			return false
		}
		if r.Response == nil || r.Response.Request == nil {
			break
		}
		r = r.Response.Request
	}
	return true
}

// originRequest walks the redirect chain back to the original request, i.e. the first request that
// was not produced by following a redirect. net/http leaves req.Response (and therefore the chain)
// nil on the original request.
func originRequest(req *http.Request) *http.Request {
	r := req
	for r.Response != nil && r.Response.Request != nil {
		r = r.Response.Request
	}
	return r
}

// idnaASCIIFromURL returns the host of u in its IDNA ASCII form.
// This is copied from net/http so that host comparisons match shouldCopyHeaderOnRedirect exactly.
//
// Source: https://github.com/golang/go/blob/go1.26.4/src/net/http/transport.go#L3024-L3030
func idnaASCIIFromURL(u *url.URL) string {
	addr := u.Hostname()
	if v, err := idna.Lookup.ToASCII(addr); err == nil {
		addr = v
	}
	return addr
}

// isDomainOrSubdomain reports whether sub is a subdomain (or exact match) of the parent domain.
// It is copied verbatim from net/http's unexported isDomainOrSubdomain to match the standard library's
// redirect header-stripping semantics exactly.
//
// Source: https://github.com/golang/go/blob/go1.26.4/src/net/http/client.go#L1028-L1045
func isDomainOrSubdomain(sub, parent string) bool {
	if sub == parent {
		return true
	}
	// If sub contains a :, it's probably an IPv6 address (and is definitely not a hostname).
	// Don't check the suffix in this case, to avoid matching the contents of a IPv6 zone.
	// For example, "::1%.www.example.com" is not a subdomain of "www.example.com".
	if strings.ContainsAny(sub, ":%") {
		return false
	}
	// If sub is "foo.example.com" and parent is "example.com",
	// that means sub must end in "."+parent.
	// Do it without allocating.
	if !strings.HasSuffix(sub, parent) {
		return false
	}
	return sub[len(sub)-len(parent)-1] == '.'
}

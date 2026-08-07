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

// setAuthorizationHeader sets the Authorization header on req to value, unless following
// redirects has taken req outside the original request origin.
func setAuthorizationHeader(req *http.Request, value string) {
	if !authHeaderAllowedOnRedirect(req) {
		return
	}
	req.Header.Set("Authorization", value)
}

var redirectSensitiveHeaders = map[string]struct{}{
	"Authorization":       {},
	"Www-Authenticate":    {},
	"Cookie":              {},
	"Cookie2":             {},
	"Proxy-Authorization": {},
	"Proxy-Authenticate":  {},
}

type redirectSensitiveHeadersMiddleware struct{}

func (redirectSensitiveHeadersMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if !authHeaderAllowedOnRedirect(req) {
		for key := range req.Header {
			if _, ok := redirectSensitiveHeaders[http.CanonicalHeaderKey(key)]; ok {
				delete(req.Header, key)
			}
		}
		if snapshot, ok := req.Context().Value(redirectSensitiveHeadersSnapshotKey{}).(http.Header); ok {
			for key, values := range snapshot {
				req.Header[key] = append([]string(nil), values...)
			}
		}
	}
	return next.RoundTrip(req)
}

type redirectSensitiveHeadersSnapshotKey struct{}

type redirectSensitiveHeadersSnapshotMiddleware struct{}

func (redirectSensitiveHeadersSnapshotMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if authHeaderAllowedOnRedirect(req) {
		return next.RoundTrip(req)
	}
	snapshot := redirectCookieSnapshot(req)
	ctx := context.WithValue(req.Context(), redirectSensitiveHeadersSnapshotKey{}, snapshot)
	return next.RoundTrip(req.WithContext(ctx))
}

func redirectCookieSnapshot(req *http.Request) http.Header {
	// If net/http may have copied origin cookies, none of the values can be proven to
	// come from the redirect target's CookieJar.
	if redirectMayContainCopiedCookies(req) {
		return nil
	}
	snapshot := make(http.Header)
	for key, values := range req.Header {
		if http.CanonicalHeaderKey(key) == "Cookie" {
			snapshot[key] = append([]string(nil), values...)
		}
	}
	return snapshot
}

func redirectMayContainCopiedCookies(req *http.Request) bool {
	origin := originRequest(req)
	if origin.URL == nil {
		return true
	}
	originHost := idnaASCIIFromURL(origin.URL)
	for r := req; r != origin; {
		if r.URL == nil {
			return true
		}
		if !isDomainOrSubdomain(idnaASCIIFromURL(r.URL), originHost) {
			return false
		}
		if r.Response == nil || r.Response.Request == nil {
			return true
		}
		r = r.Response.Request
	}
	return true
}

// Preserve the Cookie header visible before caller middleware so redirect-target CookieJar
// values survive; other sensitive headers are not restored after stripping.
func wrapTransportWithRedirectSensitiveHeaderProtection(base http.RoundTripper, middlewares ...Middleware) http.RoundTripper {
	base = wrapTransport(base, redirectSensitiveHeadersMiddleware{})
	base = wrapTransport(base, middlewares...)
	return wrapTransport(base, redirectSensitiveHeadersSnapshotMiddleware{})
}

// authHeaderAllowedOnRedirect reports whether service credentials may be attached to req.
// Unlike net/http's domain-only copy rule, service credentials are confined to the
// original scheme, host, and effective port.
func authHeaderAllowedOnRedirect(req *http.Request) bool {
	if req.Response == nil {
		// Initial request, not a redirect, credentials are always allowed.
		return true
	}
	origin := originRequest(req)
	if origin.URL == nil {
		return true
	}
	// Require every redirect to remain within the original origin. Checking every hop
	// prevents a cross-origin redirect from laundering credentials by returning later.
	for r := req; r != origin; {
		if !sameRedirectOrigin(r.URL, origin.URL) {
			return false
		}
		if r.Response == nil || r.Response.Request == nil {
			break
		}
		r = r.Response.Request
	}
	return true
}

func sameRedirectOrigin(a, b *url.URL) bool {
	return a != nil && b != nil &&
		strings.EqualFold(a.Scheme, b.Scheme) &&
		idnaASCIIFromURL(a) == idnaASCIIFromURL(b) &&
		redirectEffectivePort(a) == redirectEffectivePort(b)
}

func redirectEffectivePort(uri *url.URL) string {
	if port := uri.Port(); port != "" {
		return port
	}
	switch {
	case strings.EqualFold(uri.Scheme, "http"), strings.EqualFold(uri.Scheme, "mesh-http"):
		return "80"
	case strings.EqualFold(uri.Scheme, "https"), strings.EqualFold(uri.Scheme, "mesh-https"):
		return "443"
	default:
		return ""
	}
}

// originRequest walks the redirect chain back to the original request (i.e. the first request that
// was not produced by following a redirect).
func originRequest(req *http.Request) *http.Request {
	r := req
	for r.Response != nil && r.Response.Request != nil {
		r = r.Response.Request
	}
	return r
}

// idnaASCIIFromURL returns the host of u in its IDNA ASCII form.
//
// Source: https://github.com/golang/go/blob/go1.26.4/src/net/http/transport.go#L3024-L3030
func idnaASCIIFromURL(u *url.URL) string {
	addr := u.Hostname()
	if v, err := idna.Lookup.ToASCII(addr); err == nil {
		addr = v
	}
	return addr
}

// isDomainOrSubdomain matches net/http's redirect rule for copying sensitive headers.
func isDomainOrSubdomain(sub, parent string) bool {
	if sub == parent {
		return true
	}
	if strings.ContainsAny(sub, ":%") || !strings.HasSuffix(sub, parent) {
		return false
	}
	return sub[len(sub)-len(parent)-1] == '.'
}

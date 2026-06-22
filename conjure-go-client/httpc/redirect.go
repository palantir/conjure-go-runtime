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
	"strings"

	"golang.org/x/net/idna"
)

// authHeaderAllowedOnRedirect reports whether Authorization credentials may be attached to req.
// On the initial request it is always true; after following one or more redirects it is true only
// if every hop stayed on the same host as (or a subdomain of) the original request host. This
// mirrors net/http's own cross-host Authorization stripping so decoration does not re-attach a
// credential the standard library deliberately dropped.
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
	// Require every redirect to remain on the same host, or a subdomain of, the original host.
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

// isDomainOrSubdomain reports whether sub is a subdomain (or exact match) of the parent domain.
// It is copied verbatim from net/http's unexported isDomainOrSubdomain to match the standard
// library's redirect header-stripping semantics exactly.
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

// configuredTarget is the normalized projection of one configured base URI, shared by
// the two gates that confine a request to the service the client was built for:
// routing (a 307/308 QoS relocation is honored only if its Location matches some target
// on scheme + host + effective port + base path) and auth (a request may carry
// Authorization only if its URL matches some target's origin: scheme + host + port).
// Built once per send from the selector's base URLs.
type configuredTarget struct {
	scheme   string // mesh- prefix stripped (url.Parse lowercases the scheme)
	host     string // IDNA-ASCII
	port     string // explicit port, or the scheme default (80/443) when implicit
	basePath string // escaped base path; "" or "/" matches any path
}

// configuredTargetsFromURIs normalizes the configured base URIs into targets. A URI that
// fails to parse or has no host is skipped; a relocation matching no target is refused
// and a request to no target's origin drops Authorization, so a malformed entry can only
// be more restrictive, never less.
func configuredTargetsFromURIs(uris []string) []configuredTarget {
	targets := make([]configuredTarget, 0, len(uris))
	for _, u := range uris {
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}
		host := idnaASCIIFromURL(parsed)
		if host == "" {
			continue
		}
		targets = append(targets, configuredTarget{
			scheme:   schemeWithoutMeshPrefix(parsed.Scheme),
			host:     host,
			port:     effectivePort(parsed),
			basePath: parsed.EscapedPath(),
		})
	}
	return targets
}

// matchesRelocation reports whether a 307/308 Location targets this configured service:
// same scheme, host, and effective port, with loc's path under the base path on a
// segment boundary (so /my-service does not match /my-service-2). Base paths are
// meaningful here because sendOnce joins the endpoint path onto the configured base path.
func (t configuredTarget) matchesRelocation(loc *url.URL) bool {
	if loc == nil {
		return false
	}
	return t.scheme == schemeWithoutMeshPrefix(loc.Scheme) &&
		t.host == idnaASCIIFromURL(loc) &&
		t.port == effectivePort(loc) &&
		basePathMatchesRequest(t.basePath, loc.EscapedPath())
}

// authorizesOrigin reports whether reqURL shares this target's origin (scheme + host +
// port). Credentials are origin-scoped, not path-scoped, so the base path is not compared.
func (t configuredTarget) authorizesOrigin(reqURL *url.URL) bool {
	if reqURL == nil {
		return false
	}
	return t.scheme == schemeWithoutMeshPrefix(reqURL.Scheme) &&
		t.host == idnaASCIIFromURL(reqURL) &&
		t.port == effectivePort(reqURL)
}

// relocationAllowed reports whether a 307/308 relocation to uri is confined to a
// configured target. An unparseable uri or an empty target set is not allowed, so a
// relocation can never escape to an arbitrary host.
func relocationAllowed(uri string, targets []configuredTarget) bool {
	loc, err := url.Parse(uri)
	if err != nil {
		return false
	}
	for _, t := range targets {
		if t.matchesRelocation(loc) {
			return true
		}
	}
	return false
}

// originAuthorized reports whether reqURL shares the origin of some configured target —
// the gate a fresh request (first attempt, failover, or 307/308 relocation) must pass to
// carry Authorization.
func originAuthorized(reqURL *url.URL, targets []configuredTarget) bool {
	for _, t := range targets {
		if t.authorizesOrigin(reqURL) {
			return true
		}
	}
	return false
}

// effectivePort returns u's explicit port, or the scheme's default (80/443) when implicit,
// so https://a and https://a:443 compare equal.
func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch schemeWithoutMeshPrefix(u.Scheme) {
	case "https":
		return "443"
	case "http":
		return "80"
	default:
		return ""
	}
}

func schemeWithoutMeshPrefix(scheme string) string {
	return strings.TrimPrefix(scheme, meshSchemePrefix)
}

const meshSchemePrefix = "mesh-"

// redirectSensitiveHeaders is the set of request headers net/http strips when a redirect
// crosses to a different host (go1.26.4 net/http copyHeaders, client.go:814). Decoration
// drops these contributors and physically deletes the headers on any hop that fails
// authAllowed, so it cannot re-attach a credential the standard library dropped or carry
// one onto a 307/308 relocation. Keys are canonical so a case-insensitive lookup matches.
var redirectSensitiveHeaders = map[string]struct{}{
	"Authorization":       {},
	"Www-Authenticate":    {},
	"Cookie":              {},
	"Cookie2":             {},
	"Proxy-Authorization": {},
	"Proxy-Authenticate":  {},
}

// isRedirectSensitiveHeader reports whether key is one of the cross-host sensitive
// headers, comparing case-insensitively: a raw Runtime.Send map-write can store a
// non-canonical key (req.Header["authorization"]), which canonicalizes to a match here.
func isRedirectSensitiveHeader(key string) bool {
	_, ok := redirectSensitiveHeaders[http.CanonicalHeaderKey(key)]
	return ok
}

// withoutRedirectSensitiveHeaders returns vals with every sensitive-header contributor
// removed. Decoration re-resolves contributors on every RoundTrip, so this is how it
// avoids re-attaching auth/cookie values (builder, raw header, or per-call alike) when a
// hop is not authorized — see authHeaderAllowedOnRedirect and originAuthorized.
func withoutRedirectSensitiveHeaders(vals []requestValue[http.Header]) []requestValue[http.Header] {
	out := make([]requestValue[http.Header], 0, len(vals))
	for _, v := range vals {
		if v != nil && isRedirectSensitiveHeader(v.key()) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// deleteRedirectSensitiveHeaders physically removes any sensitive header already present
// on h, matching keys case-insensitively. This backs net/http's own cross-host strip as
// defense-in-depth and removes a raw non-canonical header (req.Header["authorization"])
// that Header.Del — which canonicalizes only its argument — would miss.
func deleteRedirectSensitiveHeaders(h http.Header) {
	for k := range h {
		if isRedirectSensitiveHeader(k) {
			delete(h, k)
		}
	}
}

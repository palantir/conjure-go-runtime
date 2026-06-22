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

// authorizedHostsFromURIs returns the IDNA-ASCII hosts of the configured base URIs. They are the
// hosts a request may carry Authorization to: the first attempt and any failover target by
// construction, and a 307/308 QoS relocation only if its Location host is one of them (or a
// subdomain) — so a relocation cannot leak the credential to a foreign host. The mesh- scheme
// prefix does not affect the parsed host.
func authorizedHostsFromURIs(uris []string) []string {
	hosts := make([]string, 0, len(uris))
	for _, u := range uris {
		parsed, err := url.Parse(u)
		if err != nil {
			continue
		}
		if h := idnaASCIIFromURL(parsed); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// hostIsAuthorized reports whether host (IDNA-ASCII) equals or is a subdomain of any authorized
// host, using net/http's verbatim isDomainOrSubdomain comparison.
func hostIsAuthorized(host string, authorized []string) bool {
	for _, a := range authorized {
		if isDomainOrSubdomain(host, a) {
			return true
		}
	}
	return false
}

// withoutAuthorization returns vals with any Authorization-keyed contributor removed. Decoration
// re-resolves every contributor on every RoundTrip, so this is how it drops auth (builder, raw
// header, or per-call alike) when following a cross-host redirect — see authHeaderAllowedOnRedirect.
func withoutAuthorization(vals []requestValue[http.Header]) []requestValue[http.Header] {
	out := make([]requestValue[http.Header], 0, len(vals))
	for _, v := range vals {
		if v != nil && v.key() == "Authorization" {
			continue
		}
		out = append(out, v)
	}
	return out
}

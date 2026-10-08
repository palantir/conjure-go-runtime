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

package refreshingclient

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// tlsFallbackRoute identifies the destination and network path on which TLS fallback was required. It is analogous to
// net/http's internal connection-pool key, but contains only the origin and proxy identity relevant to TLS fallback.
// Proxy credentials are intentionally excluded because they do not normally change TLS compatibility and must not be
// retained in cache keys.
type tlsFallbackRoute struct {
	originAuthority string
	proxyScheme     string
	proxyAuthority  string
}

// tlsFallbackRoute returns the canonical origin and proxy route for req, or nil when fallback is not configured or the
// request route cannot be determined safely. A nil route must not be cached or used for lookup.
func (t *managedTransport) tlsFallbackRoute(req *http.Request) *tlsFallbackRoute {
	if t.tls12FallbackTransport == nil || req.URL == nil || !strings.EqualFold(req.URL.Scheme, "https") {
		return nil
	}
	originAuthority := canonicalAuthority(req.URL, "443")
	if originAuthority == nil {
		return nil
	}
	route := tlsFallbackRoute{originAuthority: *originAuthority}
	if t.transport.Proxy == nil {
		return &route
	}
	proxyURL, err := t.transport.Proxy(req)
	if err != nil {
		return nil
	}
	if proxyURL == nil {
		return &route
	}
	route.proxyScheme = strings.ToLower(proxyURL.Scheme)
	proxyAuthority := canonicalAuthority(proxyURL, defaultPort(route.proxyScheme))
	if proxyAuthority == nil {
		return nil
	}
	route.proxyAuthority = *proxyAuthority
	return &route
}

// canonicalAuthority returns the URL authority as a canonical host and effective port. It returns nil when the URL has
// no hostname. Hostnames that cannot be converted to ASCII are retained and lowercased. Does not make network calls.
func canonicalAuthority(u *url.URL, fallbackPort string) *string {
	host := u.Hostname()
	if host == "" {
		return nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		host = addr.String()
	} else if asciiHost, err := idna.Lookup.ToASCII(host); err == nil {
		host = strings.ToLower(asciiHost)
	} else {
		host = strings.ToLower(host)
	}
	port := u.Port()
	if port == "" {
		port = fallbackPort
	}
	if port == "" {
		return &host
	}
	authority := net.JoinHostPort(host, port)
	return &authority
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	case "socks5", "socks5h":
		return "1080"
	default:
		return ""
	}
}

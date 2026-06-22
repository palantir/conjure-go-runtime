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
)

// decorationMiddleware resolves a request's header and query contributors onto
// each attempt before it reaches the transport. Running per attempt on the
// freshly cloned request means contributors apply to a clean slate, so retries
// never duplicate added values. Because it runs on every RoundTrip, it also gates
// the cross-host sensitive headers (Authorization, Cookie, …) against the request's
// target (see authAllowed) so neither a 301/302/303 redirect the http.Client follows
// nor a 307/308 QoS relocation leaks a credential to a host the client did not configure.
type decorationMiddleware struct {
	headerValues []requestValue[http.Header]
	queryValues  []requestValue[url.Values]
	// authorizedTargets are the configured base-URL targets a request may carry the
	// sensitive headers to, matched on origin (scheme+host+port); empty imposes no
	// restriction.
	authorizedTargets []configuredTarget
}

// authAllowed reports whether this request may carry the cross-host sensitive headers. A
// redirect hop the http.Client produced (req.Response set) must keep every hop on the
// origin host or a subdomain, mirroring net/http's own strip; any other request — the
// first attempt, a failover, or a 307/308 relocation — must share a configured target's
// origin (scheme+host+port), so a relocation cannot leak a credential to a foreign origin.
func (d decorationMiddleware) authAllowed(req *http.Request) bool {
	if req.Response != nil {
		return authHeaderAllowedOnRedirect(req)
	}
	if len(d.authorizedTargets) == 0 {
		return true
	}
	return originAuthorized(req.URL, d.authorizedTargets)
}

func (d decorationMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	headerValues := d.headerValues
	if !d.authAllowed(req) {
		// The request's target is not one the client authorized (a cross-host redirect, or
		// a hop that re-attached a credential the stdlib stripped). Decoration re-resolves
		// contributors per RoundTrip, so drop the sensitive contributors rather than
		// re-attach them, and physically delete any sensitive header already on the cloned
		// request — a raw Runtime.Send header, or one net/http re-copied from the original
		// request onto a same-domain sub-hop after an earlier cross-host hop.
		headerValues = withoutRedirectSensitiveHeaders(headerValues)
		deleteRedirectSensitiveHeaders(req.Header)
	}
	if len(headerValues) > 0 {
		if err := resolveValues(req.Context(), req.Header, headerValues...); err != nil {
			return nil, err
		}
	}
	if len(d.queryValues) > 0 {
		q := req.URL.Query()
		if err := resolveValues(req.Context(), q, d.queryValues...); err != nil {
			return nil, err
		}
		req.URL.RawQuery = q.Encode()
	}
	return next.RoundTrip(req)
}

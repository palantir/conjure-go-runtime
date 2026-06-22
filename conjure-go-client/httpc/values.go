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
	"context"
	"net/http"
	"net/url"
)

// headerOrQuery is the set of stdlib multimap types a [requestValue] targets.
// http.Header and url.Values expose the same Add/Set/Get, so header and query
// contributors share one implementation.
type headerOrQuery interface {
	http.Header | url.Values

	Add(string, string)
	Set(string, string)
	Get(string) string
}

// requestValue contributes one key's value into a request's headers or query.
// Resolution is lazy and fallible: apply runs only if the contributor survives
// precedence resolution (see [resolveValues]), so a superseded contributor's
// value is never produced — an overridden auth provider never runs or errors.
type requestValue[T headerOrQuery] interface {
	key() string
	// replaces reports whether this contributor sets (one winner per key) or
	// adds (accumulates with other contributors for the key).
	replaces() bool
	apply(ctx context.Context, dst T) error
}

// setValue replaces all values for a key (Set, then Add any extras).
type setValue[T headerOrQuery] struct {
	name   string
	values []string
}

func (v setValue[T]) key() string    { return v.name }
func (v setValue[T]) replaces() bool { return true }

func (v setValue[T]) apply(_ context.Context, dst T) error {
	if len(v.values) == 0 {
		return nil
	}
	dst.Set(v.name, v.values[0])
	for _, s := range v.values[1:] {
		dst.Add(v.name, s)
	}
	return nil
}

// addValue appends values for a key, accumulating with other contributors.
type addValue[T headerOrQuery] struct {
	name   string
	values []string
}

func (v addValue[T]) key() string    { return v.name }
func (v addValue[T]) replaces() bool { return false }

func (v addValue[T]) apply(_ context.Context, dst T) error {
	for _, s := range v.values {
		dst.Add(v.name, s)
	}
	return nil
}

// authValue contributes an Authorization header from an [Authorizer]. Like any
// replacing contributor it resolves by precedence: a higher-precedence
// Authorization contributor supersedes it, in which case its authorizer never
// runs (so a lazy, fallible auth provider never runs or errors when overridden).
// An empty result leaves Authorization unset.
type authValue struct {
	provider Authorizer
}

func (authValue) key() string    { return "Authorization" }
func (authValue) replaces() bool { return true }

func (v authValue) apply(ctx context.Context, dst http.Header) error {
	value, err := v.provider.AuthorizationHeader(ctx)
	if err != nil {
		return err
	}
	if value != "" {
		dst.Set("Authorization", value)
	}
	return nil
}

// resolveValues applies vals onto dst in order, resolving precedence by key
// before any contributor runs: for each key only the last replacing contributor
// and any contributors after it survive, so superseded contributors — including
// a lazy, fallible auth provider — never apply. Later entries win.
func resolveValues[T headerOrQuery](ctx context.Context, dst T, vals ...requestValue[T]) error {
	lastReplace := make(map[string]int)
	for i, v := range vals {
		if v != nil && v.replaces() {
			lastReplace[v.key()] = i
		}
	}
	for i, v := range vals {
		if v == nil {
			continue
		}
		// Skip a contributor superseded by a later replace for its key: an add
		// cleared by a following set, or an earlier set replaced by a later one.
		if ri, ok := lastReplace[v.key()]; ok && i < ri {
			continue
		}
		if err := v.apply(ctx, dst); err != nil {
			return err
		}
	}
	return nil
}

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

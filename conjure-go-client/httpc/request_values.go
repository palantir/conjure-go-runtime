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
	"slices"
)

// RequestValues is the public, copy-on-write decoration a [SendOptions] carries:
// the headers, query parameters, and authorization a runtime applies to each
// attempt. It is the honest form of what [Call.Execute] passes the runtime,
// so a caller using [Runtime.Send] directly can express the same decoration:
//
//	opts := httpc.SendOptions{
//	    Values: httpc.RequestValues{}.WithHeader("X-Tenant", "acme").WithAuthorization(httpc.BasicCredentials(user, pass)),
//	}
//
// Resolution semantics match request overrides: a later value for a key replaces
// (WithHeader/WithQuery) or accumulates (WithAddedHeader/WithAddedQuery), and the
// runtime resolves the merged set per attempt so retries do not duplicate added
// values. The zero value is empty and usable.
type RequestValues struct {
	headerValues []requestValue[http.Header]
	queryValues  []requestValue[url.Values]
}

// WithHeader sets a header to the given value(s), replacing any previously set or
// added values for the key.
func (v RequestValues) WithHeader(key, value string, additionalValues ...string) RequestValues {
	return v.withHeader(setValue[http.Header]{name: http.CanonicalHeaderKey(key), values: prepend(value, additionalValues)})
}

// WithAddedHeader appends one or more values to a header, accumulating with prior values for the key.
func (v RequestValues) WithAddedHeader(key, value string, additionalValues ...string) RequestValues {
	return v.withHeader(addValue[http.Header]{name: http.CanonicalHeaderKey(key), values: prepend(value, additionalValues)})
}

// WithQuery sets a query parameter to the given value(s), replacing any previously set or added values.
func (v RequestValues) WithQuery(key, value string, additionalValues ...string) RequestValues {
	return v.withQuery(setValue[url.Values]{name: key, values: prepend(value, additionalValues)})
}

// WithAddedQuery appends one or more values to a query parameter.
func (v RequestValues) WithAddedQuery(key, value string, additionalValues ...string) RequestValues {
	return v.withQuery(addValue[url.Values]{name: key, values: prepend(value, additionalValues)})
}

// WithAuthorization appends the given [Authorizer] as a trailing Authorization
// contributor. Unlike [Overrides.WithAuthorization] (a scalar that always wins),
// this is an ordered contributor — later contributors for Authorization win
// positionally, like every other RequestValues entry. A nil Authorizer is a
// no-op (RequestValues has no clear/default sentinel; use [NoAuthorization] to
// deliberately send no credentials).
func (v RequestValues) WithAuthorization(a Authorizer) RequestValues {
	if a == nil {
		return v
	}
	return v.withHeader(authValue{provider: a})
}

// Snapshot resolves the values onto fresh http.Header and url.Values using the
// same per-key precedence the standard runtime applies per attempt (later set
// wins, adds accumulate). It is read-only — it mutates no request — and is the
// intended way a custom [Runtime] or a test fake inspects the decoration carried
// in [SendOptions.Values]. ctx is passed to any lazy contributor (e.g. an auth
// provider), so its error surfaces here rather than mid-attempt.
func (v RequestValues) Snapshot(ctx context.Context) (http.Header, url.Values, error) {
	header := make(http.Header)
	if err := resolveValues(ctx, header, v.headerValues...); err != nil {
		return nil, nil, err
	}
	query := make(url.Values)
	if err := resolveValues(ctx, query, v.queryValues...); err != nil {
		return nil, nil, err
	}
	return header, query, nil
}

func (v RequestValues) withHeader(rv requestValue[http.Header]) RequestValues {
	v.headerValues = append(slices.Clone(v.headerValues), rv)
	return v
}

func (v RequestValues) withQuery(rv requestValue[url.Values]) RequestValues {
	v.queryValues = append(slices.Clone(v.queryValues), rv)
	return v
}

// concat appends other's contributors after the receiver's, so other's values
// take precedence (later wins). Used by the runtime to layer per-call values on
// top of its builder-intrinsic values.
func (v RequestValues) concat(other RequestValues) RequestValues {
	if v.isEmpty() {
		return other
	}
	if other.isEmpty() {
		return v
	}
	return RequestValues{
		headerValues: append(slices.Clone(v.headerValues), other.headerValues...),
		queryValues:  append(slices.Clone(v.queryValues), other.queryValues...),
	}
}

func (v RequestValues) isEmpty() bool {
	return len(v.headerValues) == 0 && len(v.queryValues) == 0
}

// requestValuesFromHeader lifts headers already written onto a request into
// replacing contributors, so they resolve through the same per-attempt precedence
// path (above builder-intrinsic values) instead of being overwritten by intrinsic
// decoration. Keys are canonicalized so a non-canonical raw map-write (e.g.
// req.Header["authorization"]) resolves — and is recognized by the cross-host
// sensitive-header strip — the same as a canonical Header.Set would.
func requestValuesFromHeader(h http.Header) RequestValues {
	var v RequestValues
	for name, values := range h {
		v = v.withHeader(setValue[http.Header]{name: http.CanonicalHeaderKey(name), values: values})
	}
	return v
}

func prepend(first string, rest []string) []string {
	return append([]string{first}, rest...)
}

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

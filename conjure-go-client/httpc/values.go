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
	"encoding/base64"
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

// authValue contributes the client's Authorization header. It is the lowest
// precedence Authorization contributor, so its provider runs only when nothing
// else claims the header — replacing the old set-if-absent auth middleware.
type authValue struct {
	provider authHeaderFunc
}

func (authValue) key() string    { return "Authorization" }
func (authValue) replaces() bool { return true }

func (v authValue) apply(ctx context.Context, dst http.Header) error {
	value, err := v.provider(ctx)
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
// never duplicate added values.
type decorationMiddleware struct {
	headerValues []requestValue[http.Header]
	queryValues  []requestValue[url.Values]
}

func (d decorationMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	if len(d.headerValues) > 0 {
		if err := resolveValues(req.Context(), req.Header, d.headerValues...); err != nil {
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

func bearerAuthHeader(token string) string {
	if token == "" {
		return ""
	}
	return "Bearer " + token
}

func basicAuthHeader(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

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
	"time"
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
// decoration. Keys are taken as-is (http.Header stores them canonicalized).
func requestValuesFromHeader(h http.Header) RequestValues {
	var v RequestValues
	for name, values := range h {
		v = v.withHeader(setValue[http.Header]{name: name, values: values})
	}
	return v
}

// CallPolicyOverrides is a per-send override set merged onto a runtime's default
// [CallPolicy]. Unlike CallPolicy (a final snapshot), each field tracks whether
// it was set, because zero/nil values are meaningful: a zero timeout disables the
// per-attempt timeout, and a nil max-attempts means "use the default formula".
// An unset field leaves the runtime default unchanged. The zero value overrides
// nothing.
type CallPolicyOverrides struct {
	timeout        *time.Duration
	initialBackoff *time.Duration
	maxBackoff     *time.Duration
	maxAttempts    *int
	maxAttemptsSet bool
}

// WithTimeout overrides the per-attempt timeout. A zero duration explicitly
// disables the per-attempt timeout (distinct from leaving it unset).
func (p CallPolicyOverrides) WithTimeout(d time.Duration) CallPolicyOverrides {
	p.timeout = &d
	return p
}

// WithMaxAttempts overrides total attempts. nil = default (2 per base URL);
// pointer to 0 = unlimited; n > 0 = exactly n. Calling this marks max attempts as
// overridden even when n is nil.
func (p CallPolicyOverrides) WithMaxAttempts(n *int) CallPolicyOverrides {
	p.maxAttempts = n
	p.maxAttemptsSet = true
	return p
}

// WithInitialBackoff overrides the initial retry backoff.
func (p CallPolicyOverrides) WithInitialBackoff(d time.Duration) CallPolicyOverrides {
	p.initialBackoff = &d
	return p
}

// WithMaxBackoff overrides the maximum retry backoff.
func (p CallPolicyOverrides) WithMaxBackoff(d time.Duration) CallPolicyOverrides {
	p.maxBackoff = &d
	return p
}

// applyTo returns base with each explicitly-set override applied.
func (p CallPolicyOverrides) applyTo(base CallPolicy) CallPolicy {
	if p.timeout != nil {
		base.Timeout = *p.timeout
	}
	if p.initialBackoff != nil {
		base.InitialBackoff = *p.initialBackoff
	}
	if p.maxBackoff != nil {
		base.MaxBackoff = *p.maxBackoff
	}
	if p.maxAttemptsSet {
		base.MaxAttempts = p.maxAttempts
	}
	return base
}

func prepend(first string, rest []string) []string {
	return append([]string{first}, rest...)
}

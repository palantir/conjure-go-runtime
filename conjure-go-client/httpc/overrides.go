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
	"time"

	"github.com/palantir/pkg/bytesbuffers"
)

type basicAuthOverride struct {
	user     string
	password string
}

// overrideValue carries a scalar override plus whether this layer set it. An
// unset value inherits the layer below at merge time; a set value applies even
// when it is zero/nil, which is how a per-call Overrides clears an inherited
// Endpoint default (e.g. WithDefaultTimeout / WithDefaultBasicAuth).
type overrideValue[T any] struct {
	value T
	set   bool
}

func setOverride[T any](v T) overrideValue[T] { return overrideValue[T]{value: v, set: true} }

// Overrides is caller-supplied per-request configuration that merges into an
// [Endpoint] via [Endpoint.WithOverrides]. It is the second of the two
// [RequestOverrides] layers: where the same-named methods on [Endpoint] set
// static defaults baked into the package-level descriptor, Overrides captures
// values that vary per call (e.g. headers derived from the request context),
// typically stored as a field on a generated service-client struct.
//
// At merge time the two layers compose: headers and query parameters
// accumulate across both; scalar values (timeout, error decoder, basic auth)
// are last-wins with the Overrides value taking precedence over the
// Endpoint-level default; middlewares append.
//
// All methods are copy-on-write, so Overrides is safe to share across
// goroutines.
//
// Headers and query parameters have set and add variants: WithHeader/WithQuery
// replaces all values for a key; WithAddedHeader/WithAddedQuery accumulates.
// Calling WithHeader after WithAddedHeader for the same key discards the
// added values (Set wins).
type Overrides struct {
	setHeaders   http.Header
	addHeaders   http.Header
	setQuery     url.Values
	addQuery     url.Values
	timeout      overrideValue[*time.Duration] // unset/nil = inherit; &0 = unlimited; &d = d
	errorDecoder overrideValue[ErrorDecoder]   // unset/nil = DefaultErrorDecoder; NoErrorDecoder{} = skip
	basicAuth    overrideValue[*basicAuthOverride]
	middlewares  []Middleware
	bufferPool   overrideValue[bytesbuffers.Pool]
}

// Clone returns a deep copy of the Overrides value.
func (c Overrides) Clone() Overrides {
	out := c
	if c.setHeaders != nil {
		out.setHeaders = c.setHeaders.Clone()
	}
	if c.addHeaders != nil {
		out.addHeaders = c.addHeaders.Clone()
	}
	if c.setQuery != nil {
		cp := make(url.Values, len(c.setQuery))
		for k, v := range c.setQuery {
			cp[k] = append([]string(nil), v...)
		}
		out.setQuery = cp
	}
	if c.addQuery != nil {
		cp := make(url.Values, len(c.addQuery))
		for k, v := range c.addQuery {
			cp[k] = append([]string(nil), v...)
		}
		out.addQuery = cp
	}
	if c.middlewares != nil {
		out.middlewares = make([]Middleware, len(c.middlewares))
		copy(out.middlewares, c.middlewares)
	}
	if c.timeout.value != nil {
		out.timeout.value = new(*c.timeout.value)
	}
	if c.basicAuth.value != nil {
		out.basicAuth.value = new(*c.basicAuth.value)
	}
	return out
}

// WithHeader sets a request header to the given value(s), replacing any
// previously added or set values for the key.
func (c Overrides) WithHeader(key, value string, additionalValues ...string) Overrides {
	c = c.Clone()
	if c.setHeaders == nil {
		c.setHeaders = make(http.Header)
	}
	c.setHeaders.Set(key, value)
	for _, v := range additionalValues {
		c.setHeaders.Add(key, v)
	}
	if c.addHeaders != nil {
		delete(c.addHeaders, http.CanonicalHeaderKey(key))
	}
	return c
}

// WithAddedHeader appends one or more values to a request header. Multiple
// calls with the same key accumulate values.
func (c Overrides) WithAddedHeader(key, value string, additionalValues ...string) Overrides {
	c = c.Clone()
	if c.addHeaders == nil {
		c.addHeaders = make(http.Header)
	}
	c.addHeaders.Add(key, value)
	for _, v := range additionalValues {
		c.addHeaders.Add(key, v)
	}
	return c
}

// WithQuery sets a query parameter to the given value(s), replacing any
// previously added or set values for the key.
func (c Overrides) WithQuery(key, value string, additionalValues ...string) Overrides {
	c = c.Clone()
	if c.setQuery == nil {
		c.setQuery = make(url.Values)
	}
	c.setQuery.Set(key, value)
	for _, v := range additionalValues {
		c.setQuery.Add(key, v)
	}
	if c.addQuery != nil {
		delete(c.addQuery, key)
	}
	return c
}

// WithAddedQuery appends one or more values to a query parameter. Multiple
// calls with the same key accumulate values.
func (c Overrides) WithAddedQuery(key, value string, additionalValues ...string) Overrides {
	c = c.Clone()
	if c.addQuery == nil {
		c.addQuery = make(url.Values)
	}
	c.addQuery.Add(key, value)
	for _, v := range additionalValues {
		c.addQuery.Add(key, v)
	}
	return c
}

// WithAddedQueryValues appends every key/value pair in q to the request
// query; preserves multi-value keys.
func (c Overrides) WithAddedQueryValues(q url.Values) Overrides {
	if len(q) == 0 {
		return c
	}
	c = c.Clone()
	if c.addQuery == nil {
		c.addQuery = make(url.Values, len(q))
	}
	for k, vs := range q {
		for _, v := range vs {
			c.addQuery.Add(k, v)
		}
	}
	return c
}

// WithTimeout sets a per-attempt timeout that overrides the client-level
// timeout. Honored by clients built via [Builder.Build]; for whole-call
// deadlines, use context.WithDeadline on the ctx passed to Execute. A zero
// duration means no per-attempt timeout; [Overrides.WithUnlimitedTimeout] is
// the explicit spelling. To drop this override and inherit the client timeout,
// use [Overrides.WithDefaultTimeout].
func (c Overrides) WithTimeout(d time.Duration) Overrides {
	c = c.Clone()
	c.timeout = setOverride(&d)
	return c
}

// WithUnlimitedTimeout disables the per-attempt timeout for this request,
// overriding any client-level or inherited timeout.
func (c Overrides) WithUnlimitedTimeout() Overrides {
	return c.WithTimeout(0)
}

// WithDefaultTimeout clears any endpoint- or override-level per-attempt timeout
// so the request inherits the client-level timeout.
func (c Overrides) WithDefaultTimeout() Overrides {
	c = c.Clone()
	c.timeout = setOverride[*time.Duration](nil)
	return c
}

// WithErrorDecoder sets a per-request error decoder that overrides the
// endpoint-level decoder and [DefaultErrorDecoder]. For typed Conjure errors,
// use conjureerrors.WithConjureErrorDecoder. To skip error decoding entirely
// use [Overrides.WithNoErrorDecoder]; to drop an inherited decoder and fall
// back to [DefaultErrorDecoder] use [Overrides.WithDefaultErrorDecoder].
func (c Overrides) WithErrorDecoder(d ErrorDecoder) Overrides {
	c = c.Clone()
	c.errorDecoder = setOverride(d)
	return c
}

// WithNoErrorDecoder skips error decoding entirely: [Endpoint.Execute] returns
// the raw response for every status code instead of decoding an error.
func (c Overrides) WithNoErrorDecoder() Overrides {
	return c.WithErrorDecoder(NoErrorDecoder())
}

// WithDefaultErrorDecoder clears any endpoint- or override-level error decoder
// so [Endpoint.Execute] falls back to [DefaultErrorDecoder].
func (c Overrides) WithDefaultErrorDecoder() Overrides {
	c = c.Clone()
	c.errorDecoder = setOverride[ErrorDecoder](nil)
	return c
}

// WithBasicAuth sets per-request basic auth credentials. Takes precedence
// over any Authorization header set via WithHeader (basic auth is applied
// after headers, replacing the Authorization value) and over the client-level
// auth installed by [Builder.SetBasicAuth] / [Builder.SetAuthToken]. To drop
// this override so lower-priority auth (or an explicit Authorization header)
// applies, use [Overrides.WithDefaultBasicAuth].
func (c Overrides) WithBasicAuth(user, password string) Overrides {
	c = c.Clone()
	c.basicAuth = setOverride(&basicAuthOverride{user: user, password: password})
	return c
}

// WithDefaultBasicAuth clears any endpoint- or override-level per-request basic
// auth, so the client-level auth or an explicit Authorization header applies.
func (c Overrides) WithDefaultBasicAuth() Overrides {
	c = c.Clone()
	c.basicAuth = setOverride[*basicAuthOverride](nil)
	return c
}

// WithMiddleware appends a per-request middleware to the chain.
func (c Overrides) WithMiddleware(m Middleware) Overrides {
	c = c.Clone()
	c.middlewares = append(c.middlewares, m)
	return c
}

// WithBufferPool sets a [bytesbuffers.Pool] that encoders may use to avoid
// per-request allocations. Overrides the pool set on the [Endpoint], if any.
// Passing nil clears the pool, equivalent to [Overrides.WithDefaultBufferPool].
func (c Overrides) WithBufferPool(p bytesbuffers.Pool) Overrides {
	c = c.Clone()
	c.bufferPool = setOverride(p)
	return c
}

// WithDefaultBufferPool clears any endpoint- or override-level buffer pool so
// encoders run without one.
func (c Overrides) WithDefaultBufferPool() Overrides {
	c = c.Clone()
	c.bufferPool = setOverride[bytesbuffers.Pool](nil)
	return c
}

// merge combines the receiver with o: set headers/query from o replace and
// clear matching add entries; add headers/query accumulate; the scalar
// overrides (timeout, error decoder, basic auth, buffer pool) are last-wins —
// o wins for any scalar it set, including an explicit clear (e.g.
// WithDefaultTimeout / WithDefaultBasicAuth), which is why o cleared a scalar it
// never set leaves the receiver's value intact; middlewares append.
func (c Overrides) merge(o Overrides) Overrides {
	out := c.Clone()

	for k, vs := range o.setHeaders {
		if out.setHeaders == nil {
			out.setHeaders = make(http.Header)
		}
		out.setHeaders[k] = append([]string(nil), vs...)
		if out.addHeaders != nil {
			delete(out.addHeaders, k)
		}
	}
	for k, vs := range o.addHeaders {
		for _, v := range vs {
			if out.addHeaders == nil {
				out.addHeaders = make(http.Header)
			}
			out.addHeaders.Add(k, v)
		}
	}

	for k, vs := range o.setQuery {
		if out.setQuery == nil {
			out.setQuery = make(url.Values)
		}
		out.setQuery[k] = append([]string(nil), vs...)
		if out.addQuery != nil {
			delete(out.addQuery, k)
		}
	}
	for k, vs := range o.addQuery {
		for _, v := range vs {
			if out.addQuery == nil {
				out.addQuery = make(url.Values)
			}
			out.addQuery.Add(k, v)
		}
	}

	if o.timeout.set {
		out.timeout = o.timeout
	}
	if o.errorDecoder.set {
		out.errorDecoder = o.errorDecoder
	}
	if o.basicAuth.set {
		out.basicAuth = o.basicAuth
	}
	if o.bufferPool.set {
		out.bufferPool = o.bufferPool
	}
	out.middlewares = append(out.middlewares, o.middlewares...)
	return out
}

// headerValues flattens the merged header overrides into request contributors:
// every set header first, then every added header, then basic auth as a
// trailing Authorization set (the highest per-request precedence). Emitting
// sets before adds preserves "set replaces, adds append" for a key present in
// both, and a set clears earlier adds during resolution.
func (c Overrides) headerValues() []requestValue[http.Header] {
	var values []requestValue[http.Header]
	for k, vs := range c.setHeaders {
		values = append(values, setValue[http.Header]{name: k, values: vs})
	}
	for k, vs := range c.addHeaders {
		values = append(values, addValue[http.Header]{name: k, values: vs})
	}
	if c.basicAuth.value != nil {
		values = append(values, setValue[http.Header]{
			name:   "Authorization",
			values: []string{basicAuthHeader(c.basicAuth.value.user, c.basicAuth.value.password)},
		})
	}
	return values
}

// queryValues flattens the merged query overrides into request contributors,
// sets before adds (see [Overrides.headerValues]).
func (c Overrides) queryValues() []requestValue[url.Values] {
	var values []requestValue[url.Values]
	for k, vs := range c.setQuery {
		values = append(values, setValue[url.Values]{name: k, values: vs})
	}
	for k, vs := range c.addQuery {
		values = append(values, addValue[url.Values]{name: k, values: vs})
	}
	return values
}

// requestValues bundles the merged header and query overrides into the public
// [RequestValues] the runtime resolves per attempt.
func (c Overrides) requestValues() RequestValues {
	return RequestValues{
		headerValues: c.headerValues(),
		queryValues:  c.queryValues(),
	}
}

// callPolicyOverrides maps the per-request scalar overrides onto a
// [CallPolicyOverrides]. Only a per-attempt timeout is expressible at this layer.
// A set timeout applies (a &0 disables the per-attempt timeout); an unset or
// cleared timeout (WithDefaultTimeout) leaves the runtime's timeout in force.
func (c Overrides) callPolicyOverrides() CallPolicyOverrides {
	var p CallPolicyOverrides
	if c.timeout.set && c.timeout.value != nil {
		p = p.WithTimeout(*c.timeout.value)
	}
	return p
}

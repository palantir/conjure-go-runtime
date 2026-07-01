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
	"net/url"
	"slices"
	"time"

	"github.com/palantir/pkg/bytesbuffers"
)

// RequestOverrides is the per-request configuration shared by the endpoint
// descriptors ([BodyEndpoint], [NoBodyEndpoint]), the per-invocation [Call], and
// the reusable [Overrides] bag. Every method is copy-on-write: it returns a new
// value with the override applied, so deriving variants from a shared base is safe:
//
//	base := ep.WithAddedHeader("X-Tenant", "acme")
//	v1 := base.WithAddedHeader("Api-Version", "1")
//	v2 := base.WithAddedHeader("Api-Version", "2") // base and v1 are unaffected
//
// The type parameter D is the concrete implementing type, so methods on a
// BodyEndpoint return a BodyEndpoint, methods on a Call return a Call, etc.
//
// The implementations represent two configuration layers composed at execute time:
//
//   - On a descriptor ([BodyEndpoint]/[NoBodyEndpoint]), these methods set static
//     defaults baked into the package-level descriptor (e.g. a constant
//     Accept-Language header for every call to a given RPC).
//   - On a [Call] (or an [Overrides] merged into one via [Call.WithOverrides]),
//     they capture per-invocation values (e.g. headers derived from the call site
//     context).
//
// Headers and query parameters accumulate across both layers; scalar values
// (timeout, error decoder, authorizer, buffer pool) are last-wins — the
// per-invocation layer wins for any scalar it set, including an explicit clear
// (WithDefault* / WithUnlimitedTimeout / WithNoErrorDecoder).
type RequestOverrides[D any] interface {
	// WithHeader sets a request header to the given value(s), replacing any
	// previously added or set values for the key.
	WithHeader(key, value string, additionalValues ...string) D
	// WithAddedHeader appends one or more values to a request header.
	// Multiple calls with the same key accumulate values.
	WithAddedHeader(key, value string, additionalValues ...string) D
	// WithQuery sets a query parameter to the given value(s), replacing any
	// previously added or set values for the key.
	WithQuery(key, value string, additionalValues ...string) D
	// WithAddedQuery appends one or more values to a query parameter.
	WithAddedQuery(key, value string, additionalValues ...string) D
	// WithAddedQueryValues appends every key/value pair in q to the request query.
	WithAddedQueryValues(q url.Values) D
	// WithTimeout sets a per-attempt timeout that overrides the client-level
	// timeout. The runtime applies it to each attempt via the call-scoped
	// *http.Client. A zero duration disables the per-attempt timeout
	// (WithUnlimitedTimeout is the explicit spelling). Use a context deadline for
	// a whole-call deadline that spans all retries.
	WithTimeout(time.Duration) D
	// WithUnlimitedTimeout disables the per-attempt timeout, overriding any
	// client-level or inherited timeout.
	WithUnlimitedTimeout() D
	// WithDefaultTimeout clears any timeout set here so the client-level timeout applies.
	WithDefaultTimeout() D
	// WithErrorDecoder sets a per-request error decoder; overrides the
	// endpoint-level decoder and [DefaultErrorDecoder]. For typed Conjure errors,
	// use the free function [WithConjureErrorDecoder], which keeps the
	// conjure-go-contract/errors dependency off this interface.
	WithErrorDecoder(ErrorDecoder) D
	// WithNoErrorDecoder skips error decoding entirely; [Call.Execute] returns
	// the raw response for every status code.
	WithNoErrorDecoder() D
	// WithDefaultErrorDecoder clears any decoder set here so [Call.Execute]
	// falls back to [DefaultErrorDecoder].
	WithDefaultErrorDecoder() D
	// WithAuthorization sets the per-request [Authorizer], overriding any
	// client-level auth. Pass [NoAuthorization] to deliberately send no
	// credentials for this request; a nil Authorizer clears the override
	// (identical to WithDefaultAuthorization).
	WithAuthorization(Authorizer) D
	// WithDefaultAuthorization clears any authorizer set here so lower-priority
	// client-level auth (or an explicit Authorization header) applies.
	WithDefaultAuthorization() D
	// WithMiddleware appends a per-request middleware that runs once per attempt
	// around the resolved request, inside telemetry like the builder middleware.
	WithMiddleware(Middleware) D
	// WithBufferPool sets a buffer pool that encoders may use to avoid
	// per-request allocations. Passing nil clears it (see WithDefaultBufferPool).
	// The [bytesbuffers.Pool] dependency is intentional — mocks of this interface
	// need to import it.
	WithBufferPool(bytesbuffers.Pool) D
	// WithDefaultBufferPool clears any buffer pool set here so encoders run without one.
	WithDefaultBufferPool() D
}

// overrideValue carries a scalar override plus whether this layer set it. An
// unset value inherits the layer below at merge time; a set value applies even
// when it is zero/nil, which is how a per-call Overrides clears an inherited
// descriptor default (e.g. WithDefaultTimeout / WithDefaultAuthorization).
type overrideValue[T any] struct {
	value T
	set   bool
}

func setOverride[T any](v T) overrideValue[T] { return overrideValue[T]{value: v, set: true} }

// Overrides is caller-supplied per-request configuration that merges into a
// [Call] via [Call.WithOverrides]. It is the second of the two [RequestOverrides]
// layers: where the same-named methods on a descriptor set static defaults baked
// into the package-level RPC shape, Overrides captures values that vary per call
// (e.g. headers derived from the request context), typically stored as a field
// on a generated service-client struct.
//
// At merge time the two layers compose: headers and query parameters
// accumulate across both; scalar values (timeout, error decoder, authorization,
// buffer pool) are last-wins with the Overrides value taking precedence over
// the descriptor-level default; middlewares append.
//
// All methods are copy-on-write, so Overrides is safe to share across
// goroutines.
//
// Headers and query parameters have set and add variants: WithHeader/WithQuery
// replaces all values for a key; WithAddedHeader/WithAddedQuery accumulates.
// Calling WithHeader after WithAddedHeader for the same key discards the
// added values (Set wins).
type Overrides struct {
	values       RequestValues                 // header & query contributors (resolved with later-wins precedence)
	timeout      overrideValue[*time.Duration] // unset/nil = inherit; &0 = unlimited; &d = d
	errorDecoder overrideValue[ErrorDecoder]   // unset/nil = DefaultErrorDecoder; NoErrorDecoder{} = skip
	auth         overrideValue[Authorizer]     // unset/nil = inherit; NoAuthorization() = send none
	middlewares  []Middleware
	bufferPool   overrideValue[bytesbuffers.Pool]
}

// Clone returns a deep copy of the Overrides value. (The contributor and
// middleware slices are append-only copy-on-write, so callers rarely need this;
// it remains for explicit isolation.) The Authorizer is shared by design — a
// reused authorizer must be concurrency-safe (see [Authorizer]) — so there is
// nothing to deep-copy for it.
func (c Overrides) Clone() Overrides {
	out := c
	if c.middlewares != nil {
		out.middlewares = slices.Clone(c.middlewares)
	}
	if c.timeout.value != nil {
		out.timeout.value = new(*c.timeout.value)
	}
	return out
}

// WithHeader sets a request header to the given value(s), replacing any
// previously added or set values for the key.
func (c Overrides) WithHeader(key, value string, additionalValues ...string) Overrides {
	c.values = c.values.WithHeader(key, value, additionalValues...)
	return c
}

// WithAddedHeader appends one or more values to a request header. Multiple
// calls with the same key accumulate values.
func (c Overrides) WithAddedHeader(key, value string, additionalValues ...string) Overrides {
	c.values = c.values.WithAddedHeader(key, value, additionalValues...)
	return c
}

// WithQuery sets a query parameter to the given value(s), replacing any
// previously added or set values for the key.
func (c Overrides) WithQuery(key, value string, additionalValues ...string) Overrides {
	c.values = c.values.WithQuery(key, value, additionalValues...)
	return c
}

// WithAddedQuery appends one or more values to a query parameter. Multiple
// calls with the same key accumulate values.
func (c Overrides) WithAddedQuery(key, value string, additionalValues ...string) Overrides {
	c.values = c.values.WithAddedQuery(key, value, additionalValues...)
	return c
}

// WithAddedQueryValues appends every key/value pair in q to the request
// query; preserves multi-value keys.
func (c Overrides) WithAddedQueryValues(q url.Values) Overrides {
	for k, vs := range q {
		if len(vs) == 0 {
			continue
		}
		c.values = c.values.WithAddedQuery(k, vs[0], vs[1:]...)
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
	c.timeout = setOverride[*time.Duration](nil)
	return c
}

// WithErrorDecoder sets a per-request error decoder that overrides the
// endpoint-level decoder and [DefaultErrorDecoder]. For typed Conjure errors,
// use the free function [WithConjureErrorDecoder]. To skip error decoding entirely
// use [Overrides.WithNoErrorDecoder]; to drop an inherited decoder and fall
// back to [DefaultErrorDecoder] use [Overrides.WithDefaultErrorDecoder].
func (c Overrides) WithErrorDecoder(d ErrorDecoder) Overrides {
	c.errorDecoder = setOverride(d)
	return c
}

// WithNoErrorDecoder skips error decoding entirely: [Call.Execute] returns the
// raw response for every status code instead of decoding an error.
func (c Overrides) WithNoErrorDecoder() Overrides {
	return c.WithErrorDecoder(NoErrorDecoder())
}

// WithDefaultErrorDecoder clears any endpoint- or override-level error decoder
// so [Call.Execute] falls back to [DefaultErrorDecoder].
func (c Overrides) WithDefaultErrorDecoder() Overrides {
	c.errorDecoder = setOverride[ErrorDecoder](nil)
	return c
}

// WithAuthorization sets the per-request [Authorizer]. It takes precedence over
// any Authorization header set via WithHeader (the authorizer is applied after
// headers, replacing the Authorization value) and over the client-level auth
// installed by [Builder.SetAuth]. Pass [NoAuthorization] to deliberately send no
// credentials for this request. A nil Authorizer drops the override so
// lower-priority auth (or an explicit Authorization header) applies — identical
// to [Overrides.WithDefaultAuthorization].
func (c Overrides) WithAuthorization(a Authorizer) Overrides {
	c.auth = setOverride(a)
	return c
}

// WithDefaultAuthorization clears any endpoint- or override-level authorizer, so
// the client-level auth or an explicit Authorization header applies.
func (c Overrides) WithDefaultAuthorization() Overrides {
	c.auth = setOverride[Authorizer](nil)
	return c
}

// WithMiddleware appends a per-request middleware to the chain.
func (c Overrides) WithMiddleware(m Middleware) Overrides {
	c.middlewares = append(slices.Clone(c.middlewares), m)
	return c
}

// WithBufferPool sets a [bytesbuffers.Pool] that encoders may use to avoid
// per-request allocations. Overrides the pool set on the descriptor, if any.
// Passing nil clears the pool, equivalent to [Overrides.WithDefaultBufferPool].
func (c Overrides) WithBufferPool(p bytesbuffers.Pool) Overrides {
	c.bufferPool = setOverride(p)
	return c
}

// WithDefaultBufferPool clears any endpoint- or override-level buffer pool so
// encoders run without one.
func (c Overrides) WithDefaultBufferPool() Overrides {
	c.bufferPool = setOverride[bytesbuffers.Pool](nil)
	return c
}

// merge combines the receiver with o: o's header/query contributors resolve
// after the receiver's (so o's sets replace and its adds accumulate); the
// scalar overrides (timeout, error decoder, authorizer, buffer pool) are
// last-wins — o wins for any scalar it set, including an explicit clear (e.g.
// WithDefaultTimeout / WithDefaultAuthorization), so a scalar o never set leaves the
// receiver's value intact; middlewares append.
func (c Overrides) merge(o Overrides) Overrides {
	out := c
	out.values = c.values.concat(o.values)
	if o.timeout.set {
		out.timeout = o.timeout
	}
	if o.errorDecoder.set {
		out.errorDecoder = o.errorDecoder
	}
	if o.auth.set {
		out.auth = o.auth
	}
	if o.bufferPool.set {
		out.bufferPool = o.bufferPool
	}
	out.middlewares = append(slices.Clone(c.middlewares), o.middlewares...)
	return out
}

// requestValues returns the header and query contributors the runtime resolves
// per attempt: the accumulated header/query values plus, when set, the
// authorizer as a trailing Authorization contributor (the highest per-request
// precedence, so it beats any Authorization header set via WithHeader regardless
// of order — auth is a scalar here, always emitted last, unlike RequestValues'
// ordered WithAuthorization).
func (c Overrides) requestValues() RequestValues {
	if c.auth.value == nil {
		return c.values
	}
	return c.values.withHeader(authValue{provider: c.auth.value})
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

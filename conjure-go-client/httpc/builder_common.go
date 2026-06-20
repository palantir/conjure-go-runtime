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
	"errors"
	"maps"
	"net/url"
	"slices"

	werror "github.com/palantir/witchcraft-go-error"
)

// baseBuilder is the shared Clone/Apply contract for builder interfaces. Builders
// are mutable: Apply modifies the receiver in place, so `b.Apply(p)` returns
// the same builder. Clone first if you need an independent variant.
type baseBuilder[Self baseBuilder[Self]] interface {
	Clone() Self
	Apply(...Param[Self]) Self
}

// Param is a reusable configuration function for a builder, applied via Apply:
//
//	func WithDefaults[B httpc.ServiceBuilder[B]]() httpc.Param[B] {
//	    return func(b B) B {
//	        return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	    }
//	}
type Param[B baseBuilder[B]] func(B) B

// Param0 wraps a zero-argument builder method as a Param, e.g. Param0((*Builder).DisableHTTP2).
func Param0[B baseBuilder[B]](p func(B) B) Param[B] {
	return func(b B) B { return p(b) }
}

// Param1 wraps a one-argument builder method and its argument as a Param,
// e.g. Param1((*Builder).SetTimeout, 30*time.Second).
func Param1[B baseBuilder[B], X any](p func(B, X) B, x X) Param[B] {
	return func(b B) B { return p(b, x) }
}

// Param2 wraps a two-argument builder method and its arguments as a Param,
// e.g. Param2((*Builder).SetBasicAuth, "user", "pass").
func Param2[B baseBuilder[B], X any, Y any](p func(B, X, Y) B, x X, y Y) Param[B] {
	return func(b B) B { return p(b, x, y) }
}

// ParamVarArgs wraps a variadic builder method and a slice of arguments as a Param,
// e.g. ParamVarArgs((*Builder).SetBaseURLs, []string{"https://a", "https://b"}).
func ParamVarArgs[B baseBuilder[B], X any](p func(B, ...X) B, xs []X) Param[B] {
	return func(b B) B { return p(b, xs...) }
}

// builderField identifies a builder setting that can carry a deferred,
// replaceable validation error. Re-setting the field (with a valid value, or a
// clearing call like SetNoProxy) drops its prior error, so the builder always
// reflects the current desired state rather than an append-only history.
type builderField int

const (
	fieldBaseURLs builderField = iota
	fieldHTTPProxy
	fieldSocksProxy
	fieldMaxAttempts
)

// builderErrors collects the validation errors a builder defers until Build.
// byField holds errors tied to a replaceable setting (replaced or cleared when
// that setting is set again); unscoped holds errors not tied to a single
// replaceable field (e.g. a whole-config ApplyConfig failure).
type builderErrors struct {
	byField  map[builderField]error
	unscoped []error
}

// setField records err for f, or clears any prior error for f when err is nil.
func (e *builderErrors) setField(f builderField, err error) {
	if err == nil {
		delete(e.byField, f)
		return
	}
	if e.byField == nil {
		e.byField = make(map[builderField]error)
	}
	e.byField[f] = err
}

// clearField drops any deferred error for each of the given fields.
func (e *builderErrors) clearField(fields ...builderField) {
	for _, f := range fields {
		delete(e.byField, f)
	}
}

// addUnscoped records an error not tied to a replaceable field.
func (e *builderErrors) addUnscoped(err error) {
	e.unscoped = append(e.unscoped, err)
}

func (e builderErrors) clone() builderErrors {
	return builderErrors{
		byField:  maps.Clone(e.byField),
		unscoped: slices.Clone(e.unscoped),
	}
}

// joined returns the combined deferred errors, or nil. Field errors are ordered
// by field id (then unscoped) so the message is deterministic.
func (e builderErrors) joined(ctx context.Context) error {
	fields := slices.Sorted(maps.Keys(e.byField))
	errs := make([]error, 0, len(fields)+len(e.unscoped))
	for _, f := range fields {
		errs = append(errs, e.byField[f])
	}
	errs = append(errs, e.unscoped...)
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return werror.WrapWithContextParams(ctx, errs[0], "builder configuration errors")
	default:
		return werror.WrapWithContextParams(ctx, errors.Join(errs...), "builder configuration errors")
	}
}

// parseProxyURL validates a proxy URL string against the same rules as
// ApplyConfig: it must be a request URI with one of the supported schemes.
func parseProxyURL(s, label string, schemes ...string) (*url.URL, error) {
	proxyURL, err := url.ParseRequestURI(s)
	if err != nil {
		return nil, werror.Wrap(err, "invalid "+label)
	}
	if !slices.Contains(schemes, proxyURL.Scheme) {
		return nil, werror.Error("invalid "+label+": unsupported scheme",
			werror.SafeParam("scheme", proxyURL.Scheme),
			werror.SafeParam("supportedSchemes", schemes))
	}
	return proxyURL, nil
}

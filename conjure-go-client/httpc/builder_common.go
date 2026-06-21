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

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

// Cloneable is the minimal contract a builder must satisfy to be rebuildable: it
// clones itself, returning its own concrete type. Builders are mutable, so Clone
// first to derive an independent variant; the rebuild path
// ([RebuildableRuntime.Builder]) clones the retained seed builder. Clone is the
// only generic operation on a builder, so the bound needs nothing more — Apply
// stays a concrete builder method rather than part of this contract.
type Cloneable[Self any] interface {
	Clone() Self
}

// Param is a reusable configuration function for a builder, applied via Apply:
//
//	func WithDefaults[B httpc.ServiceBuilder[B]]() httpc.Param[B] {
//	    return func(b B) B {
//	        return b.SetTimeout(30 * time.Second).SetMaxAttempts(new(3))
//	    }
//	}
type Param[B any] func(B) B

// Param0 wraps a zero-argument builder method as a Param, e.g. Param0((*Builder).DisableHTTP2).
func Param0[B any](p func(B) B) Param[B] {
	return func(b B) B { return p(b) }
}

// Param1 wraps a one-argument builder method and its argument as a Param,
// e.g. Param1((*Builder).SetTimeout, 30*time.Second).
func Param1[B any, X any](p func(B, X) B, x X) Param[B] {
	return func(b B) B { return p(b, x) }
}

// Param2 wraps a two-argument builder method and its arguments as a Param,
// e.g. Param2((*Builder).SetBasicAuth, "user", "pass").
func Param2[B any, X any, Y any](p func(B, X, Y) B, x X, y Y) Param[B] {
	return func(b B) B { return p(b, x, y) }
}

// ParamVarArgs wraps a variadic builder method and a slice of arguments as a Param,
// e.g. ParamVarArgs((*Builder).SetBaseURLs, []string{"https://a", "https://b"}).
func ParamVarArgs[B any, X any](p func(B, ...X) B, xs []X) Param[B] {
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
	// fieldConfig is the whole-config validation error from ApplyConfig /
	// ApplyConfigRefreshable. It is global: every Build* checks it, since a
	// failed ApplyConfig means the config silently did not apply.
	fieldConfig
)

// builderError yields the current deferred error for a field, or nil. Static
// fields wrap a fixed error; refreshable-backed fields evaluate their Validated
// each time, so a refreshable that recovers before Build clears the error.
type builderError func(context.Context) error

func staticBuilderError(err error) builderError {
	return func(context.Context) error { return err }
}

func validatedBuilderError[T any](v refreshable.Validated[T]) builderError {
	return func(context.Context) error {
		_, err := v.Validation()
		return err
	}
}

// builderErrors collects the validation errors a builder defers until Build,
// keyed by the field they belong to so that re-setting (or clearing) a field
// replaces its error. Errors are evaluated lazily via [builderError] providers
// so refreshable-backed fields reflect their current validity at Build time.
type builderErrors struct {
	byField map[builderField]builderError
}

// setField records a static err for f, or clears any prior error for f when err
// is nil.
func (e *builderErrors) setField(f builderField, err error) {
	if err == nil {
		e.clearField(f)
		return
	}
	e.setFieldProvider(f, staticBuilderError(err))
}

// setFieldProvider records a dynamic error provider for f, or clears f when
// provider is nil.
func (e *builderErrors) setFieldProvider(f builderField, provider builderError) {
	if provider == nil {
		e.clearField(f)
		return
	}
	if e.byField == nil {
		e.byField = make(map[builderField]builderError)
	}
	e.byField[f] = provider
}

// clearField drops any deferred error for each of the given fields.
func (e *builderErrors) clearField(fields ...builderField) {
	for _, f := range fields {
		delete(e.byField, f)
	}
}

func (e builderErrors) clone() builderErrors {
	return builderErrors{byField: maps.Clone(e.byField)}
}

// joined evaluates the deferred errors for the given fields (or every field when
// none are named) and returns them combined, or nil. Fields are evaluated in id
// order so the message is deterministic. Naming fields lets each Build* scope
// itself to the settings it actually consumes (e.g. BuildHTTPClient ignores base
// URLs); fieldConfig is named by every Build* because a failed config is global.
func (e builderErrors) joined(ctx context.Context, fields ...builderField) error {
	keys := fields
	if len(keys) == 0 {
		keys = slices.Sorted(maps.Keys(e.byField))
	} else {
		keys = slices.Clone(keys)
		slices.Sort(keys)
	}
	var errs []error
	for _, f := range keys {
		if provider, ok := e.byField[f]; ok {
			if err := provider(ctx); err != nil {
				errs = append(errs, err)
			}
		}
	}
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

// validateBaseURI parses a single base URI with the same parser config validation
// uses (url.ParseRequestURI). An empty string is invalid; callers that allow
// empties (ApplyConfig drops them) must filter before calling this.
func validateBaseURI(uri string) error {
	if _, err := url.ParseRequestURI(uri); err != nil {
		return werror.Wrap(err, "invalid base URL", werror.UnsafeParam("url", uri))
	}
	return nil
}

// validateBaseURIs validates every URI exactly as given — the strict
// direct-setter rule, with no empty-string filtering. Shared with the config
// path's per-URI check via [validateBaseURI].
func validateBaseURIs(uris []string) error {
	for _, uri := range uris {
		if err := validateBaseURI(uri); err != nil {
			return err
		}
	}
	return nil
}

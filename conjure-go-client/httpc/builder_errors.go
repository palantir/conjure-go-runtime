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
	"slices"

	"github.com/palantir/pkg/refreshable/v2"
	werror "github.com/palantir/witchcraft-go-error"
)

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

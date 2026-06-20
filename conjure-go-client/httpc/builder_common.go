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

// returns the combined deferred errors, or nil.
func builderErrors(ctx context.Context, errs []error) error {
	switch len(errs) {
	case 0:
		return nil
	case 1:
		return werror.WrapWithContextParams(ctx, errs[0], "builder configuration errors")
	default:
		return werror.WrapWithContextParams(ctx, errors.Join(errs...), "builder configuration errors")
	}
}

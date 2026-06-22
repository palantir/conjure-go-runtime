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

// Package conjureerrors provides httpc error-decoding helpers for typed Conjure
// errors. It is intentionally separate from the core httpc package so the
// [httpc.RequestOverrides] interface (and its mocks) need not import
// conjure-go-contract/errors; callers that decode custom Conjure error types opt
// in here.
//
// httpc.DefaultErrorDecoder already decodes the standard Conjure error types via
// the default registry. Use this package to register custom error types with a
// [errors.ConjureErrorDecoder].
package conjureerrors

import (
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
)

// DefaultErrorDecoderWithConjure is [httpc.DefaultErrorDecoder] using the
// provided ConjureErrorDecoder to unmarshal typed errors instead of the default
// registry.
func DefaultErrorDecoderWithConjure(ced errors.ConjureErrorDecoder) httpc.ErrorDecoder {
	return internal.NewRESTErrorDecoder(func(body []byte) (error, bool) {
		conjureErr, err := errors.UnmarshalErrorWithDecoder(ced, body)
		if err != nil {
			return nil, false
		}
		return conjureErr, true
	})
}

// WithConjureErrorDecoder sets a Conjure error decoder on any
// [httpc.RequestOverrides] value (a descriptor, [httpc.Call], or [httpc.Overrides]):
//
//	ep = conjureerrors.WithConjureErrorDecoder(ep, ced)
//
// It is a convenience for d.WithErrorDecoder(DefaultErrorDecoderWithConjure(ced)).
func WithConjureErrorDecoder[D httpc.RequestOverrides[D]](d D, ced errors.ConjureErrorDecoder) D {
	return d.WithErrorDecoder(DefaultErrorDecoderWithConjure(ced))
}

// WithParameterFormat sets the Accept-Conjure-Error-Parameter-Format header on any
// [httpc.RequestOverrides] value, asking Conjure servers to serialize error parameters
// in format:
//
//	ep = conjureerrors.WithParameterFormat(ep, errors.ConjureErrorParameterFormatJSON)
//
// The header is a best-effort hint — servers that do not understand it keep sending the
// legacy form, and decoding stays tolerant of both. To negotiate the format on every
// request instead, set [errors.AcceptConjureErrorParameterFormatHeader] on the builder
// via SetHeader. Living here (not on core httpc) keeps the conjure-go-contract/errors
// dependency off the [httpc.RequestOverrides] interface, consistent with the rest of this
// subpackage.
func WithParameterFormat[D httpc.RequestOverrides[D]](d D, format errors.ConjureErrorParameterFormat) D {
	return d.WithHeader(errors.AcceptConjureErrorParameterFormatHeader, string(format))
}

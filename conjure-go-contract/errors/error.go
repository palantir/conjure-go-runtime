// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

package errors

import (
	"github.com/palantir/pkg/uuid"
	wparams "github.com/palantir/witchcraft-go-params"
)

// Error is an error intended for transport through RPC channels such as HTTP responses.
//
// Error is represented by its error code, an error name identifying the type of error and
// an optional set of named parameters detailing the error.
type Error interface {
	error
	// Code returns an enum describing error category.
	Code() ErrorCode
	// Name returns an error name identifying error type.
	Name() string
	// InstanceID returns unique identifier of this particular error instance.
	InstanceID() uuid.UUID

	wparams.ParamStorer
}

// NewError returns new instance of an error of the specified type with provided parameters.
func NewError(errorType ErrorType, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(nil, errorType, wparams.NewParamStorer(parameters...))
}

// WrapWithNewError returns new instance of an error of the specified type with provided parameters wrapping an existing error.
func WrapWithNewError(cause error, errorType ErrorType, parameters ...wparams.ParamStorer) Error {
	return newGenericError(cause, errorType, wparams.NewParamStorer(parameters...))
}

// NewPermissionDenied returns new error instance of default permission denied type.
func NewPermissionDenied(parameters ...wparams.ParamStorer) Error {
	return WrapWithPermissionDenied(nil, parameters...)
}

// WrapWithPermissionDenied returns new error instance of default permission denied type wrapping an existing error.
func WrapWithPermissionDenied(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultPermissionDenied, parameters...)
}

// NewInvalidArgument returns new error instance of default invalid argument type.
func NewInvalidArgument(parameters ...wparams.ParamStorer) Error {
	return WrapWithInvalidArgument(nil, parameters...)
}

// WrapWithInvalidArgument returns new error instance of default invalid argument type wrapping an existing error.
func WrapWithInvalidArgument(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultInvalidArgument, parameters...)
}

// NewNotFound returns new error instance of default not found type.
func NewNotFound(parameters ...wparams.ParamStorer) Error {
	return WrapWithNotFound(nil, parameters...)
}

// WrapWithNotFound returns new error instance of default not found type wrapping an existing error.
func WrapWithNotFound(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultNotFound, parameters...)
}

// NewConflict returns new error instance of default conflict type.
func NewConflict(parameters ...wparams.ParamStorer) Error {
	return WrapWithConflict(nil, parameters...)
}

// WrapWithConflict returns new error instance of default conflict type wrapping an existing error.
func WrapWithConflict(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultConflict, parameters...)
}

// NewRequestEntityTooLarge returns new error instance of default request entity too large type.
func NewRequestEntityTooLarge(parameters ...wparams.ParamStorer) Error {
	return WrapWithRequestEntityTooLarge(nil, parameters...)
}

// WrapWithRequestEntityTooLarge returns new error instance of default request entity too large type wrapping an existing error.
func WrapWithRequestEntityTooLarge(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultConflict, parameters...)
}

// NewFailedPrecondition returns new error instance of default failed precondition type.
func NewFailedPrecondition(parameters ...wparams.ParamStorer) Error {
	return WrapWithFailedPrecondition(nil, parameters...)
}

// WrapWithFailedPrecondition returns new error instance of default failed precondition type wrapping an existing error.
func WrapWithFailedPrecondition(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultFailedPrecondition, parameters...)
}

// NewInternal returns new error instance of default internal type.
func NewInternal(parameters ...wparams.ParamStorer) Error {
	return WrapWithInternal(nil, parameters...)
}

// WrapWithInternal returns new error instance of default internal type wrapping an existing error.
func WrapWithInternal(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultInternal, parameters...)
}

// NewTimeout returns new error instance of default timeout type.
func NewTimeout(parameters ...wparams.ParamStorer) Error {
	return WrapWithTimeout(nil, parameters...)
}

// WrapWithTimeout returns new error instance of default timeout type wrapping an existing error.
func WrapWithTimeout(cause error, parameters ...wparams.ParamStorer) Error {
	return WrapWithNewError(cause, DefaultTimeout, parameters...)
}

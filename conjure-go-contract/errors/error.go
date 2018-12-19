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
	// Parameters returns a set of named parameters detailing this particular error instance,
	// for example error message.
	Parameters() map[string]interface{}
}

// NewError returns new instance of an error of the specified type with provided parameters.
func NewError(errorType ErrorType, parameters ...Param) Error {
	return newGenericError(errorType, newParameterizer(parameters...))
}

// NewPermissionDenied returns new error instance of default permission denied type.
func NewPermissionDenied(parameters ...Param) Error {
	return NewError(DefaultPermissionDenied, parameters...)
}

// NewInvalidArgument returns new error instance of default invalid argument type.
func NewInvalidArgument(parameters ...Param) Error {
	return NewError(DefaultInvalidArgument, parameters...)
}

// NewNotFound returns new error instance of default not found type.
func NewNotFound(parameters ...Param) Error {
	return NewError(DefaultNotFound, parameters...)
}

// NewConflict returns new error instance of default conflict type.
func NewConflict(parameters ...Param) Error {
	return NewError(DefaultConflict, parameters...)
}

// NewRequestEntityTooLarge returns new error instance of default request entity too large type.
func NewRequestEntityTooLarge(parameters ...Param) Error {
	return NewError(DefaultRequestEntityTooLarge, parameters...)
}

// NewFailedPrecondition returns new error instance of default failed precondition type.
func NewFailedPrecondition(parameters ...Param) Error {
	return NewError(DefaultFailedPrecondition, parameters...)
}

// NewInternal returns new error instance of default internal type.
func NewInternal(parameters ...Param) Error {
	return NewError(DefaultInternal, parameters...)
}

// NewTimeout returns new error instance of default timeout type.
func NewTimeout(parameters ...Param) Error {
	return NewError(DefaultTimeout, parameters...)
}

// UnpackError recreates an error instance from the given serializable error.
func UnpackError(se SerializableError) (e Error, err error) {
	var et ErrorType
	if et, err = NewErrorType(se.ErrorCode, se.ErrorName); err != nil {
		return nil, err
	}
	var p parameterizer
	if err = p.UnmarshalJSON([]byte(se.Parameters)); err != nil {
		return nil, err
	}
	e = genericError{
		errorType:       et,
		errorInstanceID: se.ErrorInstanceID,
		parameterizer:   p,
	}
	return e, nil
}

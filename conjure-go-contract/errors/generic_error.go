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

// Copyright 2016 Palantir Technologies. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package errors

import (
	"encoding/json"
	"fmt"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/palantir/pkg/uuid"
)

func newGenericError(errorType ErrorType, p parameterizer) genericError {
	return genericError{
		errorType:       errorType,
		errorInstanceID: uuid.NewUUID(),
		parameterizer:   p,
	}
}

// genericError is general purpose implementation the Error interface.
//
// It can only be created with exported constructors, which guarantee correctness of the data.
type genericError struct {
	errorType       ErrorType
	errorInstanceID uuid.UUID
	parameterizer
}

var (
	_ fmt.Stringer     = genericError{}
	_ Error            = genericError{}
	_ json.Marshaler   = genericError{}
	_ json.Unmarshaler = &genericError{}
)

// String representation of an error.
//
// For example:
//
//  "CONFLICT Facebook:LikeAlreadyGiven (00010203-0405-0607-0809-0a0b0c0d0e0f)".
//
func (e genericError) String() string {
	return fmt.Sprintf("%s (%s)", e.errorType, e.errorInstanceID)
}

func (e genericError) Error() string {
	return e.String()
}

func (e genericError) Code() ErrorCode {
	return e.errorType.code
}

func (e genericError) Name() string {
	return e.errorType.name
}

func (e genericError) InstanceID() uuid.UUID {
	return e.errorInstanceID
}

func (e genericError) MarshalJSON() ([]byte, error) {
	marshalledParameters, err := codecs.JSON.Marshal(e.parameterizer)
	if err != nil {
		return nil, err
	}
	return codecs.JSON.Marshal(SerializableError{
		ErrorCode:       e.errorType.code,
		ErrorName:       e.errorType.name,
		ErrorInstanceID: e.errorInstanceID,
		Parameters:      json.RawMessage(marshalledParameters),
	})
}

func (e *genericError) UnmarshalJSON(data []byte) (err error) {
	var se SerializableError
	if err := codecs.JSON.Unmarshal(data, &se); err != nil {
		return err
	}
	if e.errorType, err = NewErrorType(se.ErrorCode, se.ErrorName); err != nil {
		return err
	}
	e.errorInstanceID = se.ErrorInstanceID
	return e.parameterizer.UnmarshalJSON([]byte(se.Parameters))
}

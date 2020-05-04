// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
)

var registry = map[string]reflect.Type{
	DefaultPermissionDenied.Name():      reflect.TypeOf(genericError{}),
	DefaultInvalidArgument.Name():       reflect.TypeOf(genericError{}),
	DefaultNotFound.Name():              reflect.TypeOf(genericError{}),
	DefaultConflict.Name():              reflect.TypeOf(genericError{}),
	DefaultRequestEntityTooLarge.Name(): reflect.TypeOf(genericError{}),
	DefaultFailedPrecondition.Name():    reflect.TypeOf(genericError{}),
	DefaultInternal.Name():              reflect.TypeOf(genericError{}),
	DefaultTimeout.Name():               reflect.TypeOf(genericError{}),
}

func RegisterErrorType(name string, typ reflect.Type) {
	if existing, exists := registry[name]; exists {
		panic(fmt.Sprintf("ErrorName %q already registered as %v", name, existing))
	}
	registry[name] = typ
}

// UnmarshalError attempts to deserialize the message to a known implementation of Error.
// If the ErrorName is not recognized, a genericError is returned with all unsafe params.
// If we fail to unmarshal to a generic SerializableError or to the type specified by ErrorName, an error is returned.
func UnmarshalError(message json.RawMessage) (Error, error) {
	var se SerializableError
	if err := codecs.JSON.Unmarshal(message, &se); err != nil {
		// TODO(now) return some kind of constructed empty error (or nil?)
		return nil, err
	}
	typ, ok := registry[se.ErrorName]
	if !ok {
		// Unrecognized error name, fall back to genericError
		typ = reflect.TypeOf(genericError{})
	}

	instance := reflect.New(typ).Interface()
	err := codecs.JSON.Unmarshal(message, &instance)
	if err != nil {
		return nil, err
	}
	return instance.(Error), nil
}

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
	"fmt"
	"reflect"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	werror "github.com/palantir/witchcraft-go-error"
)

var registry = map[string]reflect.Type{}

var errorInterfaceType = reflect.TypeOf((*Error)(nil)).Elem()

// RegisterErrorType registers an error name and its go type in a global registry.
// The type should be a struct type whose pointer implements Error.
// Panics if name is already registered or *type does not implement Error.
func RegisterErrorType(name string, typ reflect.Type) {
	if existing, exists := registry[name]; exists {
		panic(fmt.Sprintf("ErrorName %q already registered as %v", name, existing))
	}
	if ptr := reflect.PtrTo(typ); !ptr.Implements(errorInterfaceType) {
		panic(fmt.Sprintf("Error type %v does not implement errors.Error interface", ptr))
	}
	registry[name] = typ
}

// UnmarshalError attempts to deserialize the message to a known implementation of Error.
// If the ErrorName is not recognized, a genericError is returned with all unsafe params.
// If we fail to unmarshal to a generic SerializableError or to the type specified by ErrorName, an error is returned.
func UnmarshalError(body []byte) (Error, error) {
	var se SerializableError
	if err := codecs.JSON.Unmarshal(body, &se); err != nil {
		return nil, werror.Wrap(err, "failed to unmarshal body as conjure error")
	}
	typ, ok := registry[se.ErrorName]
	if !ok {
		// Unrecognized error name, fall back to genericError
		typ = reflect.TypeOf(genericError{})
	}

	instance := reflect.New(typ).Interface()
	if err := codecs.JSON.Unmarshal(body, &instance); err != nil {
		// TODO(bmoylan): Do we want to be more lenient and use a genericError if this can not unmarshal?
		return nil, werror.Wrap(err, "failed to unmarshal body using registered type", werror.SafeParam("type", typ.String()))
	}

	// Cast should never panic, as we've verified in RegisterErrorType
	return instance.(Error), nil
}

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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/codecs"
	werror "github.com/palantir/witchcraft-go-error"
)

var globalRegistry = NewReflectTypeConjureErrorDecoder()

var errorInterfaceType = reflect.TypeFor[Error]()

// RegisterErrorType registers an error name and its go type in a global registry.
// The type should be a struct type whose pointer implements Error.
// Panics if name is already registered or *type does not implement Error.
func RegisterErrorType(name string, typ reflect.Type) {
	if err := globalRegistry.RegisterErrorType(name, typ); err != nil {
		panic(err.Error())
	}
}

// NewReflectTypeConjureErrorDecoder returns a new ConjureErrorDecoder that uses reflection to convert JSON errors to their go types.
func NewReflectTypeConjureErrorDecoder() *ReflectTypeConjureErrorDecoder {
	return &ReflectTypeConjureErrorDecoder{registry: make(map[string]reflect.Type)}
}

// ReflectTypeConjureErrorDecoder is a ConjureErrorDecoder that uses reflection to convert JSON errors to their go types.
// It stores a mapping of serialized error name to the go type that should be used to unmarshal the error.
type ReflectTypeConjureErrorDecoder struct {
	registry map[string]reflect.Type
}

// MustRegisterErrorTypes registers the provided error types in the decoder.
// Panics if any error type is already registered or if any type does not implement Error.
func (d *ReflectTypeConjureErrorDecoder) MustRegisterErrorTypes(errorTypes ...Error) *ReflectTypeConjureErrorDecoder {
	for _, errorType := range errorTypes {
		name := errorType.Name()
		typ := reflect.TypeOf(errorType)
		if typ.Kind() != reflect.Pointer {
			panic(fmt.Errorf("error type %v must be a pointer", typ))
		}
		if err := d.RegisterErrorType(name, typ.Elem()); err != nil {
			panic(err)
		}
	}
	return d
}

func (d *ReflectTypeConjureErrorDecoder) RegisterErrorType(name string, typ reflect.Type) error {
	if existing, exists := d.registry[name]; exists {
		return fmt.Errorf("ErrorName %v already registered as %v", name, existing)
	}
	if ptr := reflect.PointerTo(typ); !ptr.Implements(errorInterfaceType) {
		return fmt.Errorf("Error type %v does not implement errors.Error interface", ptr)
	}
	d.registry[name] = typ
	return nil
}

func (d *ReflectTypeConjureErrorDecoder) DecodeConjureError(errorName string, body []byte) (Error, error) {
	if typ, ok := d.registry[errorName]; ok {
		// Attempt to decode into the registered typed error. If the body's parameters
		// arrive in a form the typed decoder cannot handle then the typed decode will fail.
		// Rather than discard the error, fall back to genericError below, which preserves the error name,
		// code, and error instance ID.
		if cerr, err := decodeConjureErrorAs(typ, body); err == nil {
			return cerr, nil
		}
	}
	// Unrecognized error name, or the registered type could not decode the body: fall back to genericError
	return decodeConjureErrorAs(reflect.TypeFor[genericError](), body)
}

// decodeConjureErrorAs unmarshals body into a new instance of the provided type and
// returns it as an Error.
func decodeConjureErrorAs(typ reflect.Type, body []byte) (Error, error) {
	instance := reflect.New(typ).Interface()
	if err := codecs.JSON.Unmarshal(body, &instance); err != nil {
		return nil, werror.Wrap(err, "failed to unmarshal body using registered type", werror.SafeParam("type", typ.String()))
	}
	cerr, ok := instance.(Error)
	if !ok {
		return nil, werror.Error("unmarshaled type does not implement errors.Error interface", werror.SafeParam("type", typ.String()))
	}
	return cerr, nil
}

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
	"context"
	"reflect"
)

var globalRegistry = NewRegistry()

// RegisterErrorType registers an error name and its go type in a global registry.
// The type should be a struct type whose pointer implements Error.
// Panics if name is already registered or *type does not implement Error.
func RegisterErrorType(name string, typ reflect.Type) {
	MustRegisterErrorType(globalRegistry, name, typ)
}

// UnmarshalError attempts to deserialize the message to a known implementation of Error.
// Custom error types should be registered using RegisterErrorType.
// If the ErrorName is not recognized, a genericError is returned with all params marked unsafe.
// If we fail to unmarshal to a generic SerializableError or to the type specified by ErrorName, an error is returned.
func UnmarshalError(body []byte) (Error, error) {
	return globalRegistry.UnmarshalJSONError(context.TODO(), body)
}

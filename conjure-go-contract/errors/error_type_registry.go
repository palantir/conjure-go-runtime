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
	"fmt"
	"reflect"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	werror "github.com/palantir/witchcraft-go-error"
)

var errorInterfaceType = reflect.TypeOf((*Error)(nil)).Elem()

type Registry struct {
	types map[string]reflect.Type
}

func NewRegistry() *Registry {
	return new(Registry)
}

func (r *Registry) CopyFrom(other *Registry) error {
	for k, v := range other.types {
		if err := r.RegisterErrorType(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) RegisterErrorType(name string, typ reflect.Type) error {
	if r.types == nil {
		r.types = map[string]reflect.Type{}
	}
	if existing, exists := r.types[name]; exists {
		return fmt.Errorf("ErrorName %v already registered as %v", name, existing)
	}
	if ptr := reflect.PtrTo(typ); !ptr.Implements(errorInterfaceType) {
		return fmt.Errorf("Error type %v does not implement errors.Error interface", ptr)
	}
	r.types[name] = typ
	return nil
}

func (r *Registry) UnmarshalJSONError(ctx context.Context, body []byte) (Error, error) {
	var name struct {
		Name string `json:"errorName"`
	}
	if err := codecs.JSON.Unmarshal(body, &name); err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to unmarshal body as conjure error")
	}
	typ := r.getErrorByName(name.Name)
	instance, ok := reflect.New(r.getErrorByName(name.Name)).Interface().(Error)
	if !ok {
		// Cast should never fail, as we've verified in RegisterErrorType
		return nil, werror.ErrorWithContextParams(ctx, "registered type does not implement Error interface", werror.SafeParam("type", typ.String()))
	}
	if err := codecs.JSON.Unmarshal(body, &instance); err != nil {
		return nil, werror.WrapWithContextParams(ctx, err, "failed to unmarshal error using registered type", werror.SafeParam("type", typ.String()))
	}
	return instance, nil
}

func (r *Registry) getErrorByName(name string) reflect.Type {
	if r.types == nil {
		// No types registered, fall back to genericError
		return reflect.TypeOf(genericError{})
	}
	typ, ok := r.types[name]
	if !ok {
		// Unrecognized error name, fall back to genericError
		return reflect.TypeOf(genericError{})
	}
	return typ
}

func MustRegisterErrorType(registry *Registry, name string, typ reflect.Type) {
	if err := registry.RegisterErrorType(name, typ); err != nil {
		panic(err)
	}
}

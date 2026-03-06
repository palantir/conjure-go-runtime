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
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterErrorType_types(t *testing.T) {
	t.Run("error type should not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RegisterErrorType("name1", reflect.TypeOf(genericError{}))
		})
	})
	t.Run("reused error name should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"ErrorName name1 already registered as errors.genericError",
			func() {
				RegisterErrorType("name1", reflect.TypeOf(genericError{}))
			})
	})
	t.Run("pointer type should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"Error type **errors.genericError does not implement errors.Error interface",
			func() {
				RegisterErrorType("name2", reflect.TypeOf(&genericError{}))
			})
	})
	t.Run("non-error type should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"Error type *string does not implement errors.Error interface",
			func() {
				RegisterErrorType("name3", reflect.TypeOf("string"))
			})
	})
}

func TestMustRegisterErrorTypes(t *testing.T) {
	t.Run("registers multiple error types", func(t *testing.T) {
		r := NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(&error1{}, &error2{}, &error3{})
		assert.Equal(t, reflect.TypeOf(error1{}), r.registry["Error1"])
		assert.Equal(t, reflect.TypeOf(error2{}), r.registry["Error2"])
		assert.Equal(t, reflect.TypeOf(error3{}), r.registry["Error3"])
	})
	t.Run("returns same decoder for chaining", func(t *testing.T) {
		d := NewReflectTypeConjureErrorDecoder()
		r := d.MustRegisterErrorTypes(&error1{})
		assert.Same(t, d, r)
	})
	t.Run("chained calls register all types", func(t *testing.T) {
		r := NewReflectTypeConjureErrorDecoder().
			MustRegisterErrorTypes(&error1{}).
			MustRegisterErrorTypes(&error2{}, &error3{})
		assert.Equal(t, reflect.TypeOf(error1{}), r.registry["Error1"])
		assert.Equal(t, reflect.TypeOf(error2{}), r.registry["Error2"])
		assert.Equal(t, reflect.TypeOf(error3{}), r.registry["Error3"])
	})
	t.Run("no args is a no-op", func(t *testing.T) {
		r := NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes()
		assert.Empty(t, r.registry)
	})
	t.Run("duplicate name panics", func(t *testing.T) {
		assert.PanicsWithError(t,
			"ErrorName Error1 already registered as errors.error1",
			func() {
				NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(&error1{}, &error1{})
			})
	})
	t.Run("duplicate name across chained calls panics", func(t *testing.T) {
		assert.PanicsWithError(t,
			"ErrorName Error1 already registered as errors.error1",
			func() {
				NewReflectTypeConjureErrorDecoder().
					MustRegisterErrorTypes(&error1{}).
					MustRegisterErrorTypes(&error1{})
			})
	})
}

type error1 struct {
	genericError
}

func (e *error1) Name() string {
	return "Error1"
}

type error2 struct {
	genericError
}

func (e *error2) Name() string {
	return "Error2"
}

type error3 struct {
	genericError
}

func (e *error3) Name() string {
	return "Error3"
}

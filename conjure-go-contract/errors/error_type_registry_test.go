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

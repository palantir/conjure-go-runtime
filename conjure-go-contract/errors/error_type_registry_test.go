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

package errors_test

import (
	"reflect"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	"github.com/stretchr/testify/require"
)

func TestRegisterErrorType_types(t *testing.T) {
	reg := errors.NewRegistry()
	t.Run("error type should not error", func(t *testing.T) {
		err := reg.RegisterErrorType("name1", reflect.TypeOf(testErrorType{}))
		require.NoError(t, err)
	})
	t.Run("reused error name should error", func(t *testing.T) {
		err := reg.RegisterErrorType("name1", reflect.TypeOf(testErrorType{}))
		require.EqualError(t, err, "ErrorName name1 already registered as errors_test.testErrorType")
	})
	t.Run("pointer type should error", func(t *testing.T) {
		err := reg.RegisterErrorType("name2", reflect.TypeOf(&testErrorType{}))
		require.EqualError(t, err, "Error type **errors_test.testErrorType does not implement errors.Error interface")
	})
	t.Run("non-error type should error", func(t *testing.T) {
		err := reg.RegisterErrorType("name3", reflect.TypeOf("string"))
		require.EqualError(t, err, "Error type *string does not implement errors.Error interface")
	})
}

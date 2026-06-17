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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/codecs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterErrorType_types(t *testing.T) {
	t.Run("error type should not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RegisterErrorType("name1", reflect.TypeFor[genericError]())
		})
	})
	t.Run("reused error name should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"ErrorName name1 already registered as errors.genericError",
			func() {
				RegisterErrorType("name1", reflect.TypeFor[genericError]())
			})
	})
	t.Run("pointer type should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"Error type **errors.genericError does not implement errors.Error interface",
			func() {
				RegisterErrorType("name2", reflect.TypeFor[*genericError]())
			})
	})
	t.Run("non-error type should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"Error type *string does not implement errors.Error interface",
			func() {
				RegisterErrorType("name3", reflect.TypeFor[string]())
			})
	})
}

func TestMustRegisterErrorTypes(t *testing.T) {
	t.Run("registers multiple error types", func(t *testing.T) {
		r := NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(&error1{}, &error2{}, &error3{})
		assert.Equal(t, reflect.TypeFor[error1](), r.registry["Error1"])
		assert.Equal(t, reflect.TypeFor[error2](), r.registry["Error2"])
		assert.Equal(t, reflect.TypeFor[error3](), r.registry["Error3"])
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
		assert.Equal(t, reflect.TypeFor[error1](), r.registry["Error1"])
		assert.Equal(t, reflect.TypeFor[error2](), r.registry["Error2"])
		assert.Equal(t, reflect.TypeFor[error3](), r.registry["Error3"])
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

func TestDecodeConjureError_typedFallback(t *testing.T) {
	const (
		jsonBody   = `{"errorCode":"CONFLICT","errorName":"Test:TablesConflict","errorInstanceId":"ada42104-7688-4720-8e4e-72deae1cec87","parameters":{"tables":["basic"]}}`
		legacyBody = `{"errorCode":"CONFLICT","errorName":"Test:TablesConflict","errorInstanceId":"ada42104-7688-4720-8e4e-72deae1cec87","parameters":{"tables":"[basic]"}}`
	)
	decoder := NewReflectTypeConjureErrorDecoder().MustRegisterErrorTypes(new(tablesParamError))

	t.Run("JSON params decode into the typed error", func(t *testing.T) {
		cerr, err := decoder.DecodeConjureError("Test:TablesConflict", []byte(jsonBody))
		require.NoError(t, err)
		tablesParamErr, ok := cerr.(*tablesParamError)
		require.True(t, ok)
		assert.Equal(t, []string{"basic"}, tablesParamErr.Tables)
		assert.Equal(t, "Test:TablesConflict", tablesParamErr.Name())
		assert.Equal(t, Conflict, tablesParamErr.Code())
		assert.Equal(t, "ada42104-7688-4720-8e4e-72deae1cec87", cerr.InstanceID().String())
	})

	t.Run("string params fall back to a generic error", func(t *testing.T) {
		cerr, err := decoder.DecodeConjureError("Test:TablesConflict", []byte(legacyBody))
		require.NoError(t, err)
		_, ok := cerr.(*tablesParamError)
		assert.False(t, ok)
		assert.Equal(t, "Test:TablesConflict", cerr.Name())
		assert.Equal(t, Conflict, cerr.Code())
		assert.Equal(t, "ada42104-7688-4720-8e4e-72deae1cec87", cerr.InstanceID().String())
		assert.Equal(t, "[basic]", cerr.UnsafeParams()["tables"])
	})

	t.Run("unknown error name falls back to a generic error", func(t *testing.T) {
		cerr, err := decoder.DecodeConjureError("Test:Unregistered", []byte(jsonBody))
		require.NoError(t, err)
		_, ok := cerr.(*tablesParamError)
		assert.False(t, ok)
		assert.Equal(t, "Test:TablesConflict", cerr.Name())
		assert.Equal(t, Conflict, cerr.Code())
		assert.Equal(t, "ada42104-7688-4720-8e4e-72deae1cec87", cerr.InstanceID().String())
	})
}

// tablesParamError mimics a conjure-generated typed error whose parameters include a
// non-scalar field. Its UnmarshalJSON unmarshals the parameters into a typed struct and
// therefore fails when a parameter arrives in the legacy string form
// (e.g. "tables":"[basic]") rather than as a JSON array ("tables":["basic"]).
type tablesParamError struct {
	genericError
	Tables []string
}

func (e *tablesParamError) Name() string {
	return "Test:TablesConflict"
}

func (e *tablesParamError) UnmarshalJSON(data []byte) error {
	var se SerializableError
	if err := codecs.JSON.Unmarshal(data, &se); err != nil {
		return err
	}
	var params struct {
		Tables []string `json:"tables"`
	}
	if err := codecs.JSON.Unmarshal(se.Parameters, &params); err != nil {
		return err
	}
	e.Tables = params.Tables
	return e.genericError.UnmarshalJSON(data)
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

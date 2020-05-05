package errors

import (
	"github.com/palantir/pkg/uuid"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

func TestRegisterErrorType_types(t *testing.T) {
	t.Run("error type should not panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			RegisterErrorType("name1", reflect.TypeOf(genericError{}))
		})
	})
	t.Run("reused error name should panic", func(t *testing.T) {
		assert.PanicsWithValue(t,
			"ErrorName \"name1\" already registered as errors.genericError",
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
				RegisterErrorType("name2", reflect.TypeOf("string"))
			})
	})
}

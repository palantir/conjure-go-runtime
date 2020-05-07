package errors_test

import (
	"fmt"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
)

func TestNewWrappedError(t *testing.T) {
	t.Run("wrap with werror", func(t *testing.T) {
		err := werror.Error("an error", werror.UnsafeParam("stringParam", "stringValue"))
		cerr := errors.NewInternal(wparams.NewSafeParamStorer(map[string]interface{}{"intParam": 42}))

		result := errors.NewWrappedError(err, cerr)
		assert.Contains(t, result.Error(), "an error: INTERNAL Default:Internal")
		assert.Equal(t, map[string]interface{}{"intParam": 42, "errorInstanceId": cerr.InstanceID()}, result.(wparams.ParamStorer).SafeParams())
		assert.Equal(t, map[string]interface{}{"stringParam": "stringValue"}, result.(wparams.ParamStorer).UnsafeParams())
	})
	t.Run("wrap with plain error", func(t *testing.T) {
		err := fmt.Errorf("an error")
		cerr := errors.NewInternal(wparams.NewSafeParamStorer(map[string]interface{}{"intParam": 42}))

		result := errors.NewWrappedError(err, cerr)
		assert.Contains(t, result.Error(), "an error: INTERNAL Default:Internal")
		assert.Equal(t, map[string]interface{}{"intParam": 42, "errorInstanceId": cerr.InstanceID()}, result.(wparams.ParamStorer).SafeParams())
		assert.Equal(t, map[string]interface{}{}, result.(wparams.ParamStorer).UnsafeParams())
	})
}

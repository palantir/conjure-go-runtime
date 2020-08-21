package internal_test

import (
	"fmt"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestStatusCodeFromError(t *testing.T) {
	for _, test := range []struct{
		Name string
		Error error
		Expected int
		Ok bool
	}{
		{
			Name: "stdlib error",
			Error: fmt.Errorf("an error"),
			Expected: 0,
			Ok: false,
		},
		{
			Name: "conjure error",
			Error: errors.NewNotFound(),
			Expected: 404,
			Ok: true,
		},
		{
			Name: "wrapped conjure error",
			Error: werror.Wrap(errors.NewNotFound(), "not found"),
			Expected: 404,
			Ok: true,
		},
		{
			Name: "overridden conjure error is ignored",
			Error: werror.Wrap(errors.NewNotFound(), "not found", werror.SafeParam("statusCode", 500)),
			Expected: 404,
			Ok: true,
		},
		{
			Name: "werror with param",
			Error: werror.Error( "not found", werror.SafeParam("statusCode", 500)),
			Expected: 500,
			Ok: true,
		},
	}{
		t.Run(test.Name, func(t *testing.T) {
			actual, ok := internal.StatusCodeFromError(test.Error)
			assert.Equal(t, test.Expected, actual)
			assert.Equal(t, test.Ok, ok)
		})
	}
}

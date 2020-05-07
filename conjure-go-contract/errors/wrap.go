package errors

import (
	werror "github.com/palantir/witchcraft-go-error"
	wparams "github.com/palantir/witchcraft-go-params"
)

// NewWrappedError is a convenience function for adding an underlying error to a conjure error
// as additional context. This exists so that the conjure error becomes the RootCause, which is
// used to extract the conjure error for serialization when returned by a server handler.
func NewWrappedError(err error, conjureErr Error) error {
	if storer, ok := err.(wparams.ParamStorer); ok {
		return werror.Wrap(conjureErr, err.Error(), werror.Params(storer))
	}
	return werror.Wrap(conjureErr, err.Error())
}

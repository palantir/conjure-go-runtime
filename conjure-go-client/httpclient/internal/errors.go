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

package internal

import (
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
)

// StatusCodeFromError retrieves the 'statusCode' parameter from the provided werror.
// If the statusCode can not be detected from the error, ok is false.
// If the error is a (potentially wrapped) Conjure error, the conjure status code is returned.
// Otherwise, if the error is a werror with the statusCode param, the value of the statusCode parameter is returned.
//
// The default client error decoder sets the statusCode parameter on its returned errors. Note that, if a custom error
// decoder is used, this function will only return a status code for the error if the custom decoder sets a 'statusCode'
// parameter on the error.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	if conjureErr, ok := werror.RootCause(err).(errors.Error); ok {
		return conjureErr.Code().StatusCode(), true
	}
	statusCodeI, ok := werror.ParamFromError(err, "statusCode")
	if !ok {
		return 0, false
	}
	statusCode, ok = statusCodeI.(int)
	return statusCode, ok
}

// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

package httpclient

import (
	"net/http"

	"github.com/palantir/witchcraft-go-error"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
)

// errorMiddleware intercepts a round trip's response and inspects the HTTP status code.
// If the status is >= 400, we create and return a zerror with the 'statusCode' parameter
// set to the integer value from the response. In this case, the http.Response will be nil.
//
// Use StatusCodeFromError(err) to retrieve the code from the error,
// and WithDisableRestErrors() to disable this middleware on your client.
type errorMiddleware struct{}

func (m *errorMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, respErr := next.RoundTrip(req)
	if resp == nil || respErr != nil {
		// if error is already set, it is more severe than our HTTP error. Just return it.
		return resp, respErr
	}

	if err := errStatusCode(resp, respErr); err != nil {
		defer internal.DrainBody(resp)
		return nil, err
	}

	return resp, nil
}

func errStatusCode(resp *http.Response, respErr error) error {
	//TODO: Unmarshal conjure errors

	if respErr != nil || resp == nil {
		return respErr
	}
	if resp.StatusCode >= 400 {
		return werror.Error("server returned a status >= 400", werror.SafeParam("statusCode", resp.StatusCode))
	}
	return nil
}

// StatusCodeFromError retrieves the 'statusCode' parameter from the provided werror.
// If the error is not a zerror or does not have the statusCode param, ok is false.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	statusCodeI, _ := werror.ParamFromError(err, "statusCode")
	if statusCodeI == nil {
		return 0, false
	}
	statusCode, ok = statusCodeI.(int)
	return statusCode, ok
}

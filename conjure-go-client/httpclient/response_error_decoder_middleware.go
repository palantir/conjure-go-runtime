// Copyright (c) 2019 Palantir Technologies. All rights reserved.
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

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
	werror "github.com/palantir/witchcraft-go-error"
)

// ErrorDecoder implementations declare whether or not they should be used to handle certain http responses, and return
// decoded errors when invoked. Custom implementations can be used when consumers expect structured errors in response bodies.
type ErrorDecoder interface {
	// Handles returns whether or not the decoder considers the response an error.
	Handles(resp *http.Response) bool
	// DecodeError returns a decoded error, or an error encountered while trying to decode.
	// DecodeError should never return nil.
	DecodeError(resp *http.Response) error
}

// errorDecoderMiddleware intercepts a round trip's response.
// If the supplied ErrorDecoder handles the response, we return the error as decoded by ErrorDecoder.
// In this case, the *http.Response returned will be nil.
func errorDecoderMiddleware(errorDecoder ErrorDecoder) Middleware {
	return MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		resp, err := next.RoundTrip(req)
		// if error is already set, it is more severe than our HTTP error. Just return it.
		if resp == nil || err != nil {
			return nil, err
		}
		if errorDecoder.Handles(resp) {
			defer internal.DrainBody(resp)
			return nil, errorDecoder.DecodeError(resp)
		}
		return resp, nil
	})
}

// restErrorDecoder is our default error decoder.
// It handles responses of status code >= 400. In this case,
// we create and return a werror with the 'statusCode' parameter
// set to the integer value from the response.
//
// Use StatusCodeFromError(err) to retrieve the code from the error,
// and WithDisableRestErrors() to disable this middleware on your client.
type restErrorDecoder struct{}

var _ ErrorDecoder = restErrorDecoder{}

func (d restErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode >= http.StatusBadRequest
}

func (d restErrorDecoder) DecodeError(resp *http.Response) error {
	// TODO(bmoylan) unmarshal conjure error
	return werror.Error("server returned a status >= 400", werror.SafeParam("statusCode", resp.StatusCode))
}

// StatusCodeFromError retrieves the 'statusCode' parameter from the provided werror.
// If the error is not a werror or does not have the statusCode param, ok is false.
//
// The default client error decoder sets the statusCode parameter on its returned errors. Note that, if a custom error
// decoder is used, this function will only return a status code for the error if the custom decoder sets a 'statusCode'
// parameter on the error.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	statusCodeI, ok := werror.ParamFromError(err, "statusCode")
	if !ok {
		return 0, false
	}
	statusCode, ok = statusCodeI.(int)
	return statusCode, ok
}

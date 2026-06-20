// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc

import (
	"context"
	"net/http"
	"net/url"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/internal"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
)

// ErrorDecoder reports whether a response represents an error and, if so,
// converts it to a Go error.
type ErrorDecoder interface {
	// Handles returns true if DecodeError should be called on resp.
	Handles(resp *http.Response) bool
	DecodeError(resp *http.Response) error
}

// ErrEmptyURIs is returned by Build and by Client.Do when the client has no
// configured base URIs.
type ErrEmptyURIs struct{}

func (ErrEmptyURIs) Error() string {
	return "httpc: base URLs must not be empty"
}

// StatusCodeFromError returns the 'statusCode' werror parameter, or ok=false
// if the error has none. [DefaultErrorDecoder] sets this parameter; custom
// decoders may not.
func StatusCodeFromError(err error) (statusCode int, ok bool) {
	return internal.StatusCodeFromError(err)
}

// LocationFromError returns the 'location' werror parameter, or ok=false if
// the error has none. [DefaultErrorDecoder] sets this on 3xx responses that
// carry a Location header.
func LocationFromError(err error) (location string, ok bool) {
	return internal.LocationFromError(err)
}

// unwrapURLError converts a *url.Error to a werror, preserving any werror
// params already on the underlying error.
func unwrapURLError(ctx context.Context, respErr error) error {
	if respErr == nil {
		return nil
	}
	urlErr, ok := respErr.(*url.Error)
	if !ok {
		return respErr
	}
	params := []werror.Param{werror.SafeParam("requestMethod", urlErr.Op)}
	if parsedURL, _ := url.Parse(urlErr.URL); parsedURL != nil {
		params = append(params,
			werror.SafeParam("requestHost", parsedURL.Host),
			werror.UnsafeParam("requestPath", parsedURL.Path))
	}
	return werror.WrapWithContextParams(ctx, urlErr.Err, "httpc request failed", params...)
}

// DefaultErrorDecoder handles responses with status >= 307. For JSON responses
// it tries to unmarshal a Conjure error using the default registry; otherwise it
// includes the raw body as an unsafe parameter. 3xx responses also carry the
// Location header. Use [StatusCodeFromError] to read back the status. This is the
// fallback used by [Endpoint.Execute] when neither the endpoint nor [Overrides]
// provides one; to opt out, set [NoErrorDecoder] on the endpoint or overrides.
//
// To decode custom Conjure error types, use the conjureerrors sub-package
// (conjureerrors.DefaultErrorDecoderWithConjure / WithConjureErrorDecoder).
func DefaultErrorDecoder() ErrorDecoder {
	return internal.NewRESTErrorDecoder(func(body []byte) (error, bool) {
		conjureErr, err := errors.UnmarshalError(body)
		if err != nil {
			return nil, false
		}
		return conjureErr, true
	})
}

// NoErrorDecoder returns an [ErrorDecoder] that handles no responses. Set it
// on an [Endpoint] or [Overrides] to bypass [DefaultErrorDecoder] and have
// [Endpoint.Execute] return the raw response for every status code.
func NoErrorDecoder() ErrorDecoder {
	return noErrorDecoder{}
}

type noErrorDecoder struct{}

func (noErrorDecoder) Handles(*http.Response) bool { return false }
func (noErrorDecoder) DecodeError(*http.Response) error {
	return nil // unreachable; Handles always returns false.
}

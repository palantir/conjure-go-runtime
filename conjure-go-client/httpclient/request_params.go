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
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/palantir/witchcraft-go-error"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
)

// WithRPCMethodName configures the requests's context with the RPC method name, like "GetServiceRevision".
// This is read by the tracing and metrics middlewares.
func WithRPCMethodName(name string) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.configureCtx = append(b.configureCtx, func(ctx context.Context) context.Context {
			return ContextWithRPCMethodName(ctx, name)
		})
		return nil
	})
}

// WithRequestMethod sets the HTTP method of the request, e.g. GET or POST.
func WithRequestMethod(method string) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		if method == "" {
			return werror.Error("transport.RequestMethod: method can not be empty")
		}
		b.method = strings.ToUpper(method)
		return nil
	})
}

// WithPath sets the path for the request. This will be joined with
// one of the BaseURLs set on the client
func WithPath(path string) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.path = path
		return nil
	})
}

// WithPathf sets the path for the request. This will be joined with
// one of the BaseURLs set on the client
func WithPathf(format string, args ...interface{}) RequestParam {
	return WithPath(fmt.Sprintf(format, args...))
}

// WithHeader sets a header on a request.
func WithHeader(key, value string) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.headers.Set(key, value)
		return nil
	})
}

// WithQueryValues sets a header on a request.
func WithQueryValues(query url.Values) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.query = query
		return nil
	})
}

// WithRequestBody provides a struct to marshal and use
// as the request body. Encoding is handled by the impl
// passed to WithRequestBody.
// Example:
//
//     input := api.RequestInput{Foo: "bar"}
//     resp, err := client.Do(..., WithRequestBody(input, codecs.JSON), ...)
//
func WithRequestBody(input interface{}, encoder codecs.Encoder) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.bodyMiddleware.requestInput = input
		b.bodyMiddleware.requestEncoder = encoder
		b.headers.Set("Content-Type", encoder.ContentType())
		return nil
	})
}

// WithRawRequestBody uses the provided io.ReadCloser as
// the request body.
// Example:
//
//     input, _ := os.Open("file.txt")
//     resp, err := client.Do(..., WithRawRequestBody(input), ...)
//
func WithRawRequestBody(input io.ReadCloser) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.bodyMiddleware.requestInput = input
		b.bodyMiddleware.requestEncoder = nil
		b.headers.Set("Content-Type", "application/octet-stream")
		return nil
	})
}

// WithJSONRequest sets the request body to the input marshaled using the JSON codec.
func WithJSONRequest(input interface{}) RequestParam {
	return WithRequestBody(input, codecs.JSON)
}

// WithResponseBody provides a struct into which the body
// middleware will decode as the response body. Decoding is
// handled by the impl passed to WithResponseBody.
// Example:
//
//     var output api.RequestOutput
//     resp, err := client.Do(..., WithResponseBody(&output, codecs.JSON), ...)
//     return output, nil
//
// In the case of an empty response, output will be unmodified (left nil).
func WithResponseBody(output interface{}, decoder codecs.Decoder) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.bodyMiddleware.responseOutput = output
		b.bodyMiddleware.responseDecoder = decoder
		b.headers.Set("Accept", decoder.Accept())
		return nil
	})
}

// WithRawResponseBody configures the request such that the response
// body will not be read or drained after the request is executed.
// In this case, it is the responsibility of the caller to read and
// close the returned reader.
// Example:
//
//     resp, err := client.Do(..., WithRawResponseBody(), ...)
//     defer resp.Body.Close()
//     bytes, err := ioutil.ReadAll(resp.Body)
//
// In the case of an empty response, output will be unmodified (left nil).
func WithRawResponseBody() RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.bodyMiddleware.rawOutput = true
		b.bodyMiddleware.responseOutput = nil
		b.bodyMiddleware.responseDecoder = nil
		b.headers.Set("Accept", "application/octet-stream")
		return nil
	})
}

// WithJSONResponse unmarshals the response body using the JSON codec.
// The request will return an error if decoding fails.
func WithJSONResponse(output interface{}) RequestParam {
	return WithResponseBody(output, codecs.JSON)
}

// WithCompressedRequest wraps the 'codec'-encoded request body in zlib compression.
func WithCompressedRequest(input interface{}, codec codecs.Codec) RequestParam {
	return requestParamFunc(func(b *requestBuilder) error {
		b.headers.Set("Content-Encoding", "deflate")
		b.bodyMiddleware.requestInput = input
		b.bodyMiddleware.requestEncoder = codecs.ZLIB(codec)
		b.headers.Set("Content-Type", codec.ContentType())
		return nil
	})
}

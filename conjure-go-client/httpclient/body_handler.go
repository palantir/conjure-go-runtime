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
	"bytes"
	"fmt"
	"net/http"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/pkg/bytesbuffers"
	werror "github.com/palantir/witchcraft-go-error"
)

type bodyMiddleware struct {
	requestInput   interface{}
	requestEncoder codecs.Encoder

	// if rawOutput is true, the body of the response is not drained before returning -- it is the responsibility of the
	// caller to read from and properly close the response body.
	rawOutput       bool
	responseOutput  interface{}
	responseDecoder codecs.Decoder

	bufferPool bytesbuffers.Pool
}

func (b *bodyMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	cleanup, err := b.setRequestBody(req)
	if err != nil {
		return nil, err
	}

	resp, respErr := next.RoundTrip(req)
	cleanup()

	if err := b.readResponse(resp, respErr); err != nil {
		return nil, err
	}

	return resp, nil
}

// setRequestBody returns a function that should be called once the request has been completed.
func (b *bodyMiddleware) setRequestBody(req *http.Request) (func(), error) {
	if b.requestInput == nil {
		return func() {}, nil
	}

	// Special case: if the requestInput is a RequestBody and the requestEncoder is nil,
	// use the provided input directly as the request body.
	if b.requestEncoder == nil {
		requestBody, ok := b.requestInput.(RequestBody)
		if !ok {
			return nil, werror.ErrorWithContextParams(req.Context(), "requestEncoder is nil but requestInput is not RequestBody",
				werror.SafeParam("requestInputType", fmt.Sprintf("%T", b.requestInput)))
		}
		return func() {}, requestBody.setRequestBody(req)
	}

	// If buffer pool is set, use it with Encode and return a func to return the buffer to the pool.
	if b.bufferPool != nil {
		buf := b.bufferPool.Get()
		cleanup := func() {
			b.bufferPool.Put(buf)
		}
		if err := b.requestEncoder.Encode(buf, b.requestInput); err != nil {
			cleanup()
			return nil, werror.WrapWithContextParams(req.Context(), err, "failed to encode request object")
		}
		if err := RequestBodyInMemory(buf).setRequestBody(req); err != nil {
			cleanup()
			return nil, err
		}
		return cleanup, nil
	}

	// If buffer pool is not set, let Marshal allocate memory for the serialized object.
	buf, err := b.requestEncoder.Marshal(b.requestInput)
	if err != nil {
		return nil, werror.WrapWithContextParams(req.Context(), err, "failed to encode request object")
	}
	if err := RequestBodyInMemory(bytes.NewReader(buf)).setRequestBody(req); err != nil {
		return nil, err
	}
	return func() {}, nil
}

func (b *bodyMiddleware) readResponse(resp *http.Response, respErr error) error {
	// If rawOutput is true, return response directly without draining or closing body
	if b.rawOutput && respErr == nil {
		return nil
	}

	if respErr != nil {
		return respErr
	}

	// Verify we have a body to unmarshal. If the request was unsuccessful, the errorMiddleware will
	// set a non-nil error and return no response.
	if b.responseOutput == nil || resp == nil || resp.Body == nil || resp.ContentLength == 0 {
		return nil
	}

	decErr := b.responseDecoder.Decode(resp.Body, b.responseOutput)
	if decErr != nil {
		return decErr
	}

	return nil
}

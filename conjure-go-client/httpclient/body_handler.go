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
	"compress/zlib"
	"context"
	"io"
	"io/ioutil"
	"net/http"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/pkg/bytesbuffers"
	werror "github.com/palantir/witchcraft-go-error"
)

type bodyMiddleware struct {
	// Request body modes, only one can be non-nil:
	// * requestInput & requestEncoder: the encoder's Encode method is called with requestInput
	// * requestMarshaler: a reference to some object's method (e.g. MarshalJSON) for encoding bytes.
	// * requestAppender: a reference to some object's method (e.g. AppendJSON) method for appending encoded bytes to a buffer.
	requestInput       interface{}
	requestEncoder     codecs.Encoder
	requestMarshaler   func() ([]byte, error)
	requestAppender    func([]byte) ([]byte, error)
	requestCompression bool

	// Response handler modes, only one can be non-nil:
	// * rawOutput: the body of the response is not drained before returning -- the caller must read from and properly close the response body.
	// * responseOutput & responseDecoder: the decoder's Decode method is called with responseOutput (a pointer to some object)
	// * responseUnmarshaler: a reference to some object's method for decoding bytes.
	rawOutput           bool
	responseOutput      interface{}
	responseDecoder     codecs.Decoder
	responseUnmarshaler func([]byte) error
	// error if response is 200 and body is empty
	responseRequired bool

	bufferPool bytesbuffers.Pool
}

func (b *bodyMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	buf, cleanup := b.newBuffer()
	defer cleanup()

	if err := b.setRequestBody(req, buf); err != nil {
		return nil, err
	}

	resp, respErr := next.RoundTrip(req)
	if respErr != nil {
		return nil, respErr
	}
	// reset buffer to be reused for response
	buf.Reset()
	if err := b.readResponse(req.Context(), resp, buf); err != nil {
		return nil, err
	}
	return resp, nil
}

// setRequestBody returns a function that should be called once the request has been completed.
func (b *bodyMiddleware) setRequestBody(req *http.Request, buf *bytes.Buffer) error {
	var data []byte
	switch {
	case b.requestEncoder == nil && b.requestInput != nil:
		// Special case: if the requestInput is an io.ReadCloser and the requestEncoder is nil,
		// use the provided input directly as the request body.
		if bodyReadCloser, ok := b.requestInput.(io.ReadCloser); ok {
			req.Body = bodyReadCloser
			// Use the same heuristic as http.NewRequest to generate the "GetBody" function.
			if newReq, err := http.NewRequest("", "", bodyReadCloser); err == nil {
				req.GetBody = newReq.GetBody
			}
			return nil
		}
		return werror.ErrorWithContextParams(req.Context(), "requestEncoder required when requestInput is set")
	case b.requestEncoder != nil:
		if err := b.requestEncoder.Encode(buf, b.requestInput); err != nil {
			return werror.WrapWithContextParams(req.Context(), err, "encode request object")
		}
		data = buf.Bytes()
	case b.requestMarshaler != nil:
		var err error
		data, err = b.requestMarshaler()
		if err != nil {
			return werror.WrapWithContextParams(req.Context(), err, "marshal request object")
		}
	case b.requestAppender != nil:
		var err error
		data, err = b.requestAppender(buf.Bytes())
		if err != nil {
			return werror.WrapWithContextParams(req.Context(), err, "append request object")
		}
	default:
		return nil
	}

	if len(data) > 0 {
		if b.requestCompression {
			var err error
			req.Body, err = zlib.NewReader(bytes.NewReader(data))
			if err != nil {
				return err
			}
			req.GetBody = func() (io.ReadCloser, error) {
				return zlib.NewReader(bytes.NewReader(data))
			}
		} else {
			req.Body = ioutil.NopCloser(bytes.NewReader(data))
			req.ContentLength = int64(len(data))
			req.GetBody = func() (io.ReadCloser, error) {
				return ioutil.NopCloser(bytes.NewReader(data)), nil
			}
		}
	} else {
		req.Body = http.NoBody
		req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
	}
	return nil
}

func (b *bodyMiddleware) readResponse(ctx context.Context, resp *http.Response, buf *bytes.Buffer) error {
	switch {
	case resp == nil || resp.Body == nil:
		// this should never happen, but we do not want to panic
		return werror.ErrorWithContextParams(ctx, "nil response body")
	case b.rawOutput:
		// If rawOutput is true, return response directly without draining or closing body
		return nil
	case resp.Body == http.NoBody:
		if b.responseRequired && resp.StatusCode == http.StatusOK {
			return werror.ErrorWithContextParams(ctx, "empty response body")
		}
		return nil
	case b.responseDecoder != nil:
		return b.responseDecoder.Decode(resp.Body, b.responseOutput)
	case b.responseUnmarshaler != nil:
		if _, err := buf.ReadFrom(resp.Body); err != nil {
			return werror.WrapWithContextParams(ctx, err, "read from response body")
		}
		return b.responseUnmarshaler(buf.Bytes())
	}
	return nil
}

func (b *bodyMiddleware) newBuffer() (*bytes.Buffer, func()) {
	if b.bufferPool == nil {
		return new(bytes.Buffer), func() {}
	}
	buf := b.bufferPool.Get()
	return buf, func() { b.bufferPool.Put(buf) }
}

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
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"

	"github.com/golang/snappy"
	"github.com/palantir/pkg/bytesbuffers"
)

const (
	ContentTypeJSON        = "application/json"
	ContentTypeOctetStream = "application/octet-stream"
)

// BodyEncoder serializes a request body of type Req into req, setting the
// Content-Type header and request body as appropriate.
type BodyEncoder[Req any] interface {
	Encode(req *http.Request, body Req) error
}

// BodyDecoder reads the response body and returns the decoded value.
type BodyDecoder[Resp any] interface {
	Decode(ctx context.Context, resp *http.Response) (Resp, error)
}

// bodyEncoderFunc adapts a function to [BodyEncoder]; construct one via
// [NewBodyEncoderFunc].
type bodyEncoderFunc[Req any] struct {
	contentType string
	encodeFn    func(req *http.Request, body Req) error
}

// NewBodyEncoderFunc adapts a plain function to a [BodyEncoder]. If contentType
// is non-empty it is set as the Content-Type header before encode runs; pass ""
// for encoders that set Content-Type themselves (e.g. multipart).
func NewBodyEncoderFunc[Req any](contentType string, encode func(req *http.Request, body Req) error) BodyEncoder[Req] {
	return bodyEncoderFunc[Req]{contentType: contentType, encodeFn: encode}
}

func (f bodyEncoderFunc[Req]) Encode(req *http.Request, body Req) error {
	if f.contentType != "" {
		req.Header.Set("Content-Type", f.contentType)
	}
	return f.encodeFn(req, body)
}

// bodyDecoderFunc adapts a function to [BodyDecoder]; construct one via
// [NewBodyDecoderFunc].
type bodyDecoderFunc[Resp any] struct {
	decodeFn func(ctx context.Context, resp *http.Response) (Resp, error)
}

// NewBodyDecoderFunc adapts a plain function to a [BodyDecoder].
func NewBodyDecoderFunc[Resp any](decode func(ctx context.Context, resp *http.Response) (Resp, error)) BodyDecoder[Resp] {
	return bodyDecoderFunc[Resp]{decodeFn: decode}
}

func (f bodyDecoderFunc[Resp]) Decode(ctx context.Context, resp *http.Response) (Resp, error) {
	return f.decodeFn(ctx, resp)
}

type bufferPoolKey struct{}

func bufferPoolFromContext(ctx context.Context) bytesbuffers.Pool {
	pool, _ := ctx.Value(bufferPoolKey{}).(bytesbuffers.Pool)
	return pool
}

func contextWithBufferPool(ctx context.Context, pool bytesbuffers.Pool) context.Context {
	return context.WithValue(ctx, bufferPoolKey{}, pool)
}

// JSONEncoder returns a BodyEncoder that serializes the request body as JSON
// and sets Content-Type to "application/json". When a buffer pool is available
// via the request context (injected by [Call.Execute] from the call),
// it is used to avoid per-request allocations.
func JSONEncoder[Req any]() BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req](ContentTypeJSON, func(req *http.Request, body Req) error {
		pool := bufferPoolFromContext(req.Context())
		if pool != nil {
			buf := pool.Get()
			buf.Reset()
			if err := json.NewEncoder(buf).Encode(body); err != nil {
				pool.Put(buf)
				return err
			}
			// Trim json.Encoder's trailing newline so output matches json.Marshal.
			trimmed := bytes.TrimRight(buf.Bytes(), "\n")
			// Copy out of the pool buffer so we can return it now; the retry
			// loop may re-read req.Body via GetBody at any later point.
			data := make([]byte, len(trimmed))
			copy(data, trimmed)
			pool.Put(buf)

			req.ContentLength = int64(len(data))
			req.Body = io.NopCloser(bytes.NewReader(data))
			req.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(bytes.NewReader(data)), nil
			}
			return nil
		}
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		req.Body = io.NopCloser(bytes.NewReader(data))
		req.ContentLength = int64(len(data))
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
		return nil
	})
}

// JSONDecoder deserializes the response body as JSON.
func JSONDecoder[Resp any]() BodyDecoder[Resp] {
	return NewBodyDecoderFunc[Resp](func(_ context.Context, resp *http.Response) (Resp, error) {
		var result Resp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return result, err
		}
		return result, nil
	})
}

// OptionalJSONDecoder deserializes the response body as JSON, returning nil on
// 204 No Content.
func OptionalJSONDecoder[Resp any]() BodyDecoder[*Resp] {
	return NewBodyDecoderFunc[*Resp](func(_ context.Context, resp *http.Response) (*Resp, error) {
		if resp.StatusCode == http.StatusNoContent {
			return nil, resp.Body.Close()
		}
		var result Resp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, err
		}
		return &result, nil
	})
}

// VoidDecoder discards the response body.
func VoidDecoder() BodyDecoder[struct{}] {
	return DiscardDecoder[struct{}]()
}

// DiscardDecoder discards the response body and returns the zero value of T.
func DiscardDecoder[T any]() BodyDecoder[T] {
	return NewBodyDecoderFunc[T](func(_ context.Context, resp *http.Response) (T, error) {
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		return *new(T), nil
	})
}

// rawBodyDecoder marks decoders that hand the body to the caller. [Call.Execute]
// does not drain the body when the decoder satisfies this interface.
type rawBodyDecoder interface {
	rawBody()
}

type binaryDecoderFunc struct {
	bodyDecoderFunc[io.ReadCloser]
}

func (binaryDecoderFunc) rawBody() {}

// BinaryDecoder returns the response body as an io.ReadCloser. The caller is
// responsible for closing it.
func BinaryDecoder() BodyDecoder[io.ReadCloser] {
	return binaryDecoderFunc{
		bodyDecoderFunc: bodyDecoderFunc[io.ReadCloser]{decodeFn: func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
			return resp.Body, nil
		}},
	}
}

// OptionalBinaryDecoder returns the response body as an io.ReadCloser, or nil
// on 204 No Content. The caller is responsible for closing the reader.
func OptionalBinaryDecoder() BodyDecoder[io.ReadCloser] {
	return binaryDecoderFunc{
		bodyDecoderFunc: bodyDecoderFunc[io.ReadCloser]{decodeFn: func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
			if resp.StatusCode == http.StatusNoContent {
				return nil, resp.Body.Close()
			}
			return resp.Body, nil
		}},
	}
}

// BinaryEncoder sends the io.ReadCloser as the request body with the given
// Content-Type. The encoder probes the body for two optional capabilities:
//   - Stat() fs.FileInfo: sets Content-Length from the size (adjusted for the
//     current offset if the body is also an io.Seeker).
//   - Name() string + io.Seeker: enables GetBody, which reopens the file and
//     seeks back to the starting offset on retry.
//
// An *os.File satisfies both, so passing one yields Content-Length and a
// retryable request. Otherwise, Content-Length is -1 (chunked) and GetBody is
// nil (not retryable).
func BinaryEncoder(contentType string) BodyEncoder[io.ReadCloser] {
	return NewBodyEncoderFunc[io.ReadCloser](contentType, func(req *http.Request, body io.ReadCloser) error {
		req.Body = body
		req.ContentLength = -1

		var startOffset int64
		seeker, seekable := body.(io.Seeker)
		if seekable {
			off, err := seeker.Seek(0, io.SeekCurrent)
			if err != nil {
				seekable = false
			} else {
				startOffset = off
			}
		}

		if s, ok := body.(fs.File); ok {
			if info, err := s.Stat(); err == nil && !info.IsDir() {
				req.ContentLength = info.Size() - startOffset
			}
		}

		if seekable {
			type namedFile interface {
				Name() string
			}
			if named, ok := body.(namedFile); ok && named.Name() != "" {
				name := named.Name()
				req.GetBody = func() (io.ReadCloser, error) {
					f, err := os.Open(name)
					if err != nil {
						return nil, err
					}
					if _, err := f.Seek(startOffset, io.SeekStart); err != nil {
						_ = f.Close()
						return nil, err
					}
					return f, nil
				}
			}
		}
		return nil
	})
}

// BinaryEncoderOnce sends the io.ReadCloser as the request body once. Unlike
// [BinaryEncoder] it does not probe for Stat/Seek/Name, so Content-Length is
// always -1 (chunked) and GetBody is nil (the request is not retryable).
// Use this for non-seekable single-use streams.
func BinaryEncoderOnce(contentType string) BodyEncoder[io.ReadCloser] {
	return NewBodyEncoderFunc[io.ReadCloser](contentType, func(req *http.Request, body io.ReadCloser) error {
		req.Body = body
		req.ContentLength = -1
		return nil
	})
}

// BinaryEncoderWithReplay sends the body produced by bodyFn with the given
// Content-Type. GetBody is set to bodyFn, making the request retryable.
// Content-Length is left at -1 (chunked transfer encoding).
func BinaryEncoderWithReplay(contentType string) BodyEncoder[func() (io.ReadCloser, error)] {
	return NewBodyEncoderFunc[func() (io.ReadCloser, error)](contentType, func(req *http.Request, bodyFn func() (io.ReadCloser, error)) error {
		body, err := bodyFn()
		if err != nil {
			return err
		}
		req.Body = body
		req.ContentLength = -1
		req.GetBody = bodyFn
		return nil
	})
}

// ZLIBEncoder wraps inner with zlib (deflate) compression and sets Content-Encoding: deflate.
func ZLIBEncoder[Req any](inner BodyEncoder[Req]) BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req]("", func(req *http.Request, body Req) error {
		if err := inner.Encode(req, body); err != nil {
			return err
		}
		return compressRequestBody(req, "deflate", func(w io.Writer) io.WriteCloser {
			return zlib.NewWriter(w)
		})
	})
}

// SnappyEncoder wraps inner with Snappy compression and sets Content-Encoding: snappy.
func SnappyEncoder[Req any](inner BodyEncoder[Req]) BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req]("", func(req *http.Request, body Req) error {
		if err := inner.Encode(req, body); err != nil {
			return err
		}
		return compressRequestBody(req, "snappy", func(w io.Writer) io.WriteCloser {
			return snappy.NewBufferedWriter(w)
		})
	})
}

// GZIPEncoder wraps inner with gzip compression and sets Content-Encoding: gzip.
func GZIPEncoder[Req any](inner BodyEncoder[Req]) BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req]("", func(req *http.Request, body Req) error {
		if err := inner.Encode(req, body); err != nil {
			return err
		}
		return compressRequestBody(req, "gzip", func(w io.Writer) io.WriteCloser {
			return gzip.NewWriter(w)
		})
	})
}

// compressRequestBody wraps req.Body in a streaming compressor and sets
// Content-Encoding. Compressed size is unknown so ContentLength becomes -1
// (chunked). Retryability is preserved: if the original GetBody is set, the
// new one wraps a fresh inner body in a fresh compressor.
func compressRequestBody(req *http.Request, encoding string, newWriter func(io.Writer) io.WriteCloser) error {
	if req.Body == nil {
		return nil
	}
	req.Header.Set("Content-Encoding", encoding)
	req.ContentLength = -1

	origBody := req.Body
	req.Body = newCompressedReader(origBody, newWriter)

	if origGetBody := req.GetBody; origGetBody != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			body, err := origGetBody()
			if err != nil {
				return nil, err
			}
			return newCompressedReader(body, newWriter), nil
		}
	} else {
		req.GetBody = nil
	}
	return nil
}

// newCompressedReader streams src through the compressor produced by newWriter.
// A background goroutine drives compression; closing the returned reader
// cancels it and releases src.
func newCompressedReader(src io.ReadCloser, newWriter func(io.Writer) io.WriteCloser) io.ReadCloser {
	pr, pw := io.Pipe()
	go func() {
		defer func() { _ = src.Close() }()
		w := newWriter(pw)
		_, copyErr := io.Copy(w, src)
		closeErr := w.Close()
		if copyErr != nil {
			_ = pw.CloseWithError(copyErr)
		} else {
			_ = pw.CloseWithError(closeErr)
		}
	}()
	return pr
}

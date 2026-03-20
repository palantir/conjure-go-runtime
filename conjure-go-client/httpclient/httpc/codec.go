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

	"github.com/golang/snappy"
	"github.com/palantir/pkg/bytesbuffers"
)

const (
	ContentTypeJSON        = "application/json"
	ContentTypeOctetStream = "application/octet-stream"
)

// BodyEncoder encodes a request body of type Req into an HTTP request.
// Implementations set the Content-Type header on req directly if appropriate.
// This gives encoders full control over the header value and timing — for example,
// a multipart encoder can set the boundary parameter after generating it.
type BodyEncoder[Req any] interface {
	// Encode serializes body and writes it into req, setting the Content-Type
	// header and request body as needed.
	Encode(req *http.Request, body Req) error
}

// BodyDecoder decodes an HTTP response body into a value of type Resp.
// The Accept header (if any) is not the decoder's responsibility; set it on
// the Endpoint via SetAccept.
type BodyDecoder[Resp any] interface {
	// Decode reads the response body and returns the decoded value.
	Decode(ctx context.Context, resp *http.Response) (Resp, error)
}

// BodyEncoderFunc is a convenience adapter for the BodyEncoder interface.
// If contentType is non-empty, it is set as the Content-Type header before
// calling the encode function.
type BodyEncoderFunc[Req any] struct {
	contentType string
	encodeFn    func(req *http.Request, body Req) error
}

// NewBodyEncoderFunc creates a BodyEncoderFunc. If contentType is non-empty,
// Encode sets it as the Content-Type header before calling encode.
// Pass "" for encoders that set Content-Type themselves (e.g., multipart).
func NewBodyEncoderFunc[Req any](contentType string, encode func(req *http.Request, body Req) error) BodyEncoderFunc[Req] {
	return BodyEncoderFunc[Req]{contentType: contentType, encodeFn: encode}
}

// Encode implements BodyEncoder. Sets Content-Type (if configured) then delegates
// to the encode function.
func (f BodyEncoderFunc[Req]) Encode(req *http.Request, body Req) error {
	if f.contentType != "" {
		req.Header.Set("Content-Type", f.contentType)
	}
	return f.encodeFn(req, body)
}

// BodyDecoderFunc is a function adapter for the BodyDecoder interface.
type BodyDecoderFunc[Resp any] struct {
	decodeFn func(ctx context.Context, resp *http.Response) (Resp, error)
}

// NewBodyDecoderFunc creates a BodyDecoderFunc backed by the given function.
func NewBodyDecoderFunc[Resp any](decode func(ctx context.Context, resp *http.Response) (Resp, error)) BodyDecoderFunc[Resp] {
	return BodyDecoderFunc[Resp]{decodeFn: decode}
}

// Decode implements BodyDecoder.
func (f BodyDecoderFunc[Resp]) Decode(ctx context.Context, resp *http.Response) (Resp, error) {
	return f.decodeFn(ctx, resp)
}

// poolProvider is implemented by clients that provide a buffer pool.
// Endpoint.Execute type-asserts the client to check for pool availability.
type poolProvider interface {
	getBufferPool() bytesbuffers.Pool
}

// bufferPoolKey is the context key for the buffer pool injected by Endpoint.Execute.
type bufferPoolKey struct{}

// bufferPoolFromContext retrieves the buffer pool from the context, if set.
func bufferPoolFromContext(ctx context.Context) bytesbuffers.Pool {
	pool, _ := ctx.Value(bufferPoolKey{}).(bytesbuffers.Pool)
	return pool
}

// JSONEncoder returns a BodyEncoder that serializes the request body as JSON
// and sets Content-Type to "application/json". When a buffer pool is available
// via the request context (injected by Endpoint.Execute from the client),
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
			// json.Encoder.Encode appends a trailing newline; trim it for consistency with json.Marshal.
			trimmed := bytes.TrimRight(buf.Bytes(), "\n")
			// Copy out of pool buffer so it can be returned immediately.
			// This avoids tying the buffer lifetime to the request body,
			// which the retry loop may replace via GetBody.
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

// JSONDecoder returns a BodyDecoder that deserializes the response body from JSON.
// Use SetAccept("application/json") on the Endpoint to set the Accept header.
func JSONDecoder[Resp any]() BodyDecoder[Resp] {
	return NewBodyDecoderFunc[Resp](func(_ context.Context, resp *http.Response) (Resp, error) {
		var result Resp
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return result, err
		}
		return result, nil
	})
}

// OptionalJSONDecoder returns a BodyDecoder that deserializes the response body from JSON,
// returning nil when the response has no content.
// Use SetAccept("application/json") on the Endpoint to set the Accept header.
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

// VoidDecoder returns a BodyDecoder that discards the response body.
func VoidDecoder() BodyDecoder[struct{}] {
	return NewBodyDecoderFunc[struct{}](func(_ context.Context, resp *http.Response) (struct{}, error) {
		if resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		return struct{}{}, nil
	})
}

// rawBodyDecoder is implemented by decoders that return the response body
// directly to the caller. Endpoint.Execute will not drain the body after
// calling such a decoder.
type rawBodyDecoder interface {
	rawBody()
}

// binaryDecoderFunc is a BodyDecoderFunc that implements rawBodyDecoder.
type binaryDecoderFunc struct {
	BodyDecoderFunc[io.ReadCloser]
}

func (binaryDecoderFunc) rawBody() {}

// BinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
// The caller is responsible for closing the returned reader.
func BinaryDecoder() BodyDecoder[io.ReadCloser] {
	return binaryDecoderFunc{
		BodyDecoderFunc: NewBodyDecoderFunc[io.ReadCloser](func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
			return resp.Body, nil
		}),
	}
}

// OptionalBinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser,
// returning nil when the response has no content.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
// The caller is responsible for closing the returned reader.
func OptionalBinaryDecoder() BodyDecoder[io.ReadCloser] {
	return binaryDecoderFunc{
		BodyDecoderFunc: NewBodyDecoderFunc[io.ReadCloser](func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
			if resp.StatusCode == http.StatusNoContent {
				return nil, resp.Body.Close()
			}
			return resp.Body, nil
		}),
	}
}

// BinaryEncoder returns a BodyEncoder that sets the request body to the provided
// io.ReadCloser. Content-Type is set to the provided value.
//
// The encoder probes the body for additional capabilities:
//   - If the body implements fs.File (or any interface with a Stat() method returning
//     fs.FileInfo), Content-Length is set from FileInfo.Size(), adjusted for the
//     current read offset if the body is also an io.Seeker.
//   - If the body implements io.Seeker, GetBody is set to a function that seeks
//     back to the initial offset, making the request retryable.
//   - If neither is satisfied, Content-Length is -1 (chunked) and GetBody is nil
//     (not retryable).
//
// An *os.File satisfies both, so passing one gives both Content-Length and retryability.
func BinaryEncoder(contentType string) BodyEncoder[io.ReadCloser] {
	return NewBodyEncoderFunc[io.ReadCloser](contentType, func(req *http.Request, body io.ReadCloser) error {
		req.Body = body
		req.ContentLength = -1

		// Probe for io.Seeker to record starting offset and enable replay.
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

		// Probe for Stat() to set Content-Length.
		type statter interface {
			Stat() (fs.FileInfo, error)
		}
		if s, ok := body.(statter); ok {
			if info, err := s.Stat(); err == nil && !info.IsDir() {
				req.ContentLength = info.Size() - startOffset
			}
		}

		// Set GetBody for retryability if the body is seekable.
		if seekable {
			req.GetBody = func() (io.ReadCloser, error) {
				if _, err := seeker.Seek(startOffset, io.SeekStart); err != nil {
					return nil, err
				}
				return body, nil
			}
		}
		return nil
	})
}

// BinaryEncoderWithReplay returns a BodyEncoder that sets the request body from a
// func() (io.ReadCloser, error) factory. GetBody IS set using the factory,
// so the request IS retryable. Content-Type is set to the provided value.
// Content-Length is not set (chunked transfer encoding).
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

// ZLIBEncoder wraps a BodyEncoder with zlib (deflate) compression.
// Sets Content-Encoding: deflate. Content-Type is preserved from the inner encoder.
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

// SnappyEncoder wraps a BodyEncoder with Snappy compression.
// Sets Content-Encoding: snappy. Content-Type is preserved from the inner encoder.
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

// GZIPEncoder wraps a BodyEncoder with gzip compression.
// Sets Content-Encoding: gzip. Content-Type is preserved from the inner encoder.
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

// compressRequestBody replaces req.Body with a streaming compressed reader
// that pipes the original body through the compressor on-the-fly. Sets the
// Content-Encoding header and ContentLength to -1 (chunked), since the
// compressed size is not known ahead of time.
//
// If the original request had a GetBody (meaning the uncompressed body is
// replayable), the compressed body is also replayable: GetBody returns a
// fresh compressed stream wrapping a fresh uncompressed body. If the original
// body was not replayable, GetBody is nil and the request is not retryable.
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

// newCompressedReader returns an io.ReadCloser that streams src through the
// compressor produced by newWriter. A background goroutine drives the
// compression; closing the returned reader cancels the goroutine and releases
// all resources.
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

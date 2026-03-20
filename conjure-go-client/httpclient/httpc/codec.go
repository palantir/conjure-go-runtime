package httpc

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/palantir/pkg/bytesbuffers"
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

// JSONEncoder returns a BodyEncoder that serializes the request body as JSON
// and sets Content-Type to "application/json".
func JSONEncoder[Req any]() BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req]("application/json", func(req *http.Request, body Req) error {
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

// JSONEncoderWithPool returns a BodyEncoder that serializes the request body as JSON
// using a buffer from the provided pool. The buffer is returned to the pool when the
// request body is closed. This avoids per-request allocations in high-throughput scenarios.
// Content-Type is set to "application/json".
func JSONEncoderWithPool[Req any](pool bytesbuffers.Pool) BodyEncoder[Req] {
	return NewBodyEncoderFunc[Req]("application/json", func(req *http.Request, body Req) error {
		buf := pool.Get()
		buf.Reset()
		if err := json.NewEncoder(buf).Encode(body); err != nil {
			pool.Put(buf)
			return err
		}
		// json.Encoder.Encode appends a trailing newline; trim it for consistency with json.Marshal.
		data := bytes.TrimRight(buf.Bytes(), "\n")
		req.ContentLength = int64(len(data))
		req.Body = &poolReturnCloser{Reader: bytes.NewReader(data), buf: buf, pool: pool}
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(data)), nil
		}
		return nil
	})
}

// poolReturnCloser wraps a bytes.Reader and returns the underlying buffer to the pool on Close.
type poolReturnCloser struct {
	*bytes.Reader
	buf  *bytes.Buffer
	pool bytesbuffers.Pool
}

func (p *poolReturnCloser) Close() error {
	if p.buf != nil {
		p.pool.Put(p.buf)
		p.buf = nil
	}
	return nil
}

// JSONDecoder returns a BodyDecoder that deserializes the response body from JSON.
// Use SetAccept("application/json") on the Endpoint to set the Accept header.
func JSONDecoder[Resp any]() BodyDecoder[Resp] {
	return NewBodyDecoderFunc[Resp](func(_ context.Context, resp *http.Response) (Resp, error) {
		// TODO Accept headers, bytesbuffer
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
		if resp.ContentLength == 0 || resp.StatusCode == http.StatusNoContent {
			return nil, nil
		}
		// TODO Accept headers, bytesbuffer
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

// BinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
func BinaryDecoder() BodyDecoder[io.ReadCloser] {
	return NewBodyDecoderFunc[io.ReadCloser](func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
		return resp.Body, nil
	})
}

// OptionalBinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser,
// returning nil when the response has no content.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
func OptionalBinaryDecoder() BodyDecoder[io.ReadCloser] {
	return NewBodyDecoderFunc[io.ReadCloser](func(_ context.Context, resp *http.Response) (io.ReadCloser, error) {
		if resp.ContentLength == 0 || resp.StatusCode == http.StatusNoContent {
			return nil, nil
		}
		return resp.Body, nil
	})
}

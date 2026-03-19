package httpc

import (
	"context"
	"io"
	"net/http"
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
	panic("not implemented")
}

// JSONDecoder returns a BodyDecoder that deserializes the response body from JSON.
// Use SetAccept("application/json") on the Endpoint to set the Accept header.
func JSONDecoder[Resp any]() BodyDecoder[Resp] {
	panic("not implemented")
}

// OptionalJSONDecoder returns a BodyDecoder that deserializes the response body from JSON,
// returning nil when the response has no content.
// Use SetAccept("application/json") on the Endpoint to set the Accept header.
func OptionalJSONDecoder[Resp any]() BodyDecoder[*Resp] {
	panic("not implemented")
}

// VoidDecoder returns a BodyDecoder that discards the response body.
func VoidDecoder() BodyDecoder[struct{}] {
	panic("not implemented")
}

// BinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
func BinaryDecoder() BodyDecoder[io.ReadCloser] {
	panic("not implemented")
}

// OptionalBinaryDecoder returns a BodyDecoder that returns the response body as an io.ReadCloser,
// returning nil when the response has no content.
// Use SetAccept("application/octet-stream") on the Endpoint to set the Accept header.
func OptionalBinaryDecoder() BodyDecoder[io.ReadCloser] {
	panic("not implemented")
}

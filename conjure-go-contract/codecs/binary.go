package codecs

import (
	"bytes"
	"io"

	"github.com/palantir/witchcraft-go-error"
)

const (
	contentTypeBinary = "application/octet-stream"
)

// Binary codec encodes and decodes binary requests and responses.
// Decode/Unmarshal accepts an io.Writer and copies content to the writer.
// Encode/Marshal accepts an io.Reader and copies content from the reader.
var Binary Codec = codecBinary{}

type codecBinary struct{}

func (codecBinary) Accept() string {
	return contentTypeBinary
}

func (codecBinary) Decode(r io.Reader, v interface{}) error {
	w, ok := v.(io.Writer)
	if !ok {
		return werror.Error("failed to decode binary data into type which does not implement io.Writer")
	}
	if closer, ok := r.(io.ReadCloser); ok {
		defer func() { _ = closer.Close() }()
	}
	if _, err := io.Copy(w, r); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func (c codecBinary) Unmarshal(data []byte, v interface{}) error {
	return c.Decode(bytes.NewReader(data), v)
}

func (codecBinary) ContentType() string {
	return contentTypeBinary
}

func (codecBinary) Encode(w io.Writer, v interface{}) error {
	r, ok := v.(io.Reader)
	if !ok {
		return werror.Error("failed to encode binary data from type which does not implement io.Reader")
	}
	if closer, ok := r.(io.ReadCloser); ok {
		defer func() { _ = closer.Close() }()
	}
	if _, err := io.Copy(w, r); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func (c codecBinary) Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := c.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

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

package httpc_test

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/httpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testPayload struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestJSONEncoder(t *testing.T) {
	enc := httpc.JSONEncoder[testPayload]()
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	err = enc.Encode(req, testPayload{Name: "foo", Value: 42})
	require.NoError(t, err)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"foo","value":42}`, string(body))
	assert.Greater(t, req.ContentLength, int64(0))

	// Verify GetBody works for replay.
	require.NotNil(t, req.GetBody)
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayBody, err := io.ReadAll(replay)
	require.NoError(t, err)
	assert.Equal(t, body, replayBody)
}

func TestJSONDecoder(t *testing.T) {
	dec := httpc.JSONDecoder[testPayload]()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"name":"bar","value":99}`)),
	}
	result, err := dec.Decode(context.Background(), resp)
	require.NoError(t, err)
	assert.Equal(t, testPayload{Name: "bar", Value: 99}, result)
}

func TestOptionalJSONDecoder(t *testing.T) {
	dec := httpc.OptionalJSONDecoder[testPayload]()

	t.Run("no content", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("with content", func(t *testing.T) {
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: 26,
			Body:          io.NopCloser(strings.NewReader(`{"name":"baz","value":100}`)),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPayload{Name: "baz", Value: 100}, *result)
	})

	t.Run("ContentLength zero value does not suppress body", func(t *testing.T) {
		// ContentLength 0 is the int64 zero value, which can happen when
		// middleware or test code constructs an http.Response without setting it.
		// The decoder should still try to read and decode the body.
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"name":"zero","value":0}`)),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Equal(t, testPayload{Name: "zero", Value: 0}, *result)
	})
}

func TestVoidDecoder(t *testing.T) {
	dec := httpc.VoidDecoder()
	body := io.NopCloser(strings.NewReader("some body content"))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       body,
	}
	result, err := dec.Decode(context.Background(), resp)
	require.NoError(t, err)
	assert.Equal(t, struct{}{}, result)
}

func TestBinaryDecoder(t *testing.T) {
	dec := httpc.BinaryDecoder()
	expected := "binary content"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(expected)),
	}
	result, err := dec.Decode(context.Background(), resp)
	require.NoError(t, err)
	require.NotNil(t, result)
	data, err := io.ReadAll(result)
	require.NoError(t, err)
	assert.Equal(t, expected, string(data))
}

func TestOptionalBinaryDecoder(t *testing.T) {
	dec := httpc.OptionalBinaryDecoder()

	t.Run("no content", func(t *testing.T) {
		resp := &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("with content", func(t *testing.T) {
		expected := "binary data"
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: int64(len(expected)),
			Body:          io.NopCloser(strings.NewReader(expected)),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, result)
		data, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expected, string(data))
	})

	t.Run("ContentLength zero value does not suppress body", func(t *testing.T) {
		expected := "binary data"
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(expected)),
		}
		result, err := dec.Decode(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, result)
		data, err := io.ReadAll(result)
		require.NoError(t, err)
		assert.Equal(t, expected, string(data))
	})
}

// trackingPool is a test bytesbuffers.Pool that tracks Get/Put calls.
type trackingPool struct {
	pool    sync.Pool
	gets    int
	puts    int
	lastPut *bytes.Buffer
}

func newTrackingPool() *trackingPool {
	p := &trackingPool{}
	p.pool.New = func() interface{} { return new(bytes.Buffer) }
	return p
}

func (p *trackingPool) Get() *bytes.Buffer {
	p.gets++
	return p.pool.Get().(*bytes.Buffer)
}

func (p *trackingPool) Put(buf *bytes.Buffer) {
	p.puts++
	p.lastPut = buf
	p.pool.Put(buf)
}

func TestZLIBEncoder(t *testing.T) {
	enc := httpc.ZLIBEncoder(httpc.JSONEncoder[testPayload]())
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	err = enc.Encode(req, testPayload{Name: "zlib", Value: 1})
	require.NoError(t, err)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "deflate", req.Header.Get("Content-Encoding"))
	assert.Equal(t, int64(-1), req.ContentLength, "streaming compression should use chunked encoding")

	// Decompress and verify.
	reader, err := zlib.NewReader(req.Body)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"zlib","value":1}`, string(decompressed))

	// Verify GetBody is set (inner JSONEncoder provides GetBody, so compression preserves it).
	require.NotNil(t, req.GetBody)
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayReader, err := zlib.NewReader(replay)
	require.NoError(t, err)
	replayDecompressed, err := io.ReadAll(replayReader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"zlib","value":1}`, string(replayDecompressed))
}

func TestSnappyEncoder(t *testing.T) {
	enc := httpc.SnappyEncoder(httpc.JSONEncoder[testPayload]())
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	err = enc.Encode(req, testPayload{Name: "snappy", Value: 2})
	require.NoError(t, err)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "snappy", req.Header.Get("Content-Encoding"))
	assert.Equal(t, int64(-1), req.ContentLength, "streaming compression should use chunked encoding")

	// Decompress and verify.
	decompressed, err := io.ReadAll(snappy.NewReader(req.Body))
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"snappy","value":2}`, string(decompressed))
}

func TestGZIPEncoder(t *testing.T) {
	enc := httpc.GZIPEncoder(httpc.JSONEncoder[testPayload]())
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	err = enc.Encode(req, testPayload{Name: "gzip", Value: 3})
	require.NoError(t, err)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "gzip", req.Header.Get("Content-Encoding"))
	assert.Equal(t, int64(-1), req.ContentLength, "streaming compression should use chunked encoding")

	// Decompress and verify.
	reader, err := gzip.NewReader(req.Body)
	require.NoError(t, err)
	decompressed, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"gzip","value":3}`, string(decompressed))
}

func TestBinaryEncoder(t *testing.T) {
	t.Run("plain ReadCloser", func(t *testing.T) {
		enc := httpc.BinaryEncoder("application/octet-stream")
		req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
		require.NoError(t, err)

		body := io.NopCloser(strings.NewReader("binary data"))
		err = enc.Encode(req, body)
		require.NoError(t, err)

		assert.Equal(t, httpc.ContentTypeOctetStream, req.Header.Get("Content-Type"))
		assert.Equal(t, int64(-1), req.ContentLength, "plain ReadCloser should not have Content-Length")
		assert.Nil(t, req.GetBody, "plain ReadCloser should not have GetBody")

		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, "binary data", string(data))
	})

	t.Run("os.File has Content-Length and GetBody", func(t *testing.T) {
		enc := httpc.BinaryEncoder(httpc.ContentTypeOctetStream)
		content := "file content here"
		f := createTempFile(t, content)

		req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
		require.NoError(t, err)
		err = enc.Encode(req, f)
		require.NoError(t, err)

		assert.Equal(t, int64(len(content)), req.ContentLength, "should set Content-Length from Stat()")
		require.NotNil(t, req.GetBody, "should set GetBody via Seek")

		// Read body.
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))

		// Replay via GetBody.
		replay, err := req.GetBody()
		require.NoError(t, err)
		replayData, err := io.ReadAll(replay)
		require.NoError(t, err)
		assert.Equal(t, content, string(replayData))
	})

	t.Run("os.File at non-zero offset", func(t *testing.T) {
		enc := httpc.BinaryEncoder(httpc.ContentTypeOctetStream)
		content := "HEADER:payload"
		f := createTempFile(t, content)
		// Advance past "HEADER:" (7 bytes).
		_, err := f.Seek(7, io.SeekStart)
		require.NoError(t, err)

		req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
		require.NoError(t, err)
		err = enc.Encode(req, f)
		require.NoError(t, err)

		assert.Equal(t, int64(len("payload")), req.ContentLength, "Content-Length should be file size minus offset")
		require.NotNil(t, req.GetBody)

		// Read body — should only get "payload".
		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(data))

		// Replay seeks back to offset 7, not 0.
		replay, err := req.GetBody()
		require.NoError(t, err)
		replayData, err := io.ReadAll(replay)
		require.NoError(t, err)
		assert.Equal(t, "payload", string(replayData))
	})

	t.Run("seeker without Stat", func(t *testing.T) {
		enc := httpc.BinaryEncoder("text/plain")
		// readSeekCloser implements io.ReadSeekCloser but not fs.File.
		body := newReadSeekCloser([]byte("seekable"))

		req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
		require.NoError(t, err)
		err = enc.Encode(req, body)
		require.NoError(t, err)

		assert.Equal(t, int64(-1), req.ContentLength, "no Stat() means no Content-Length")
		require.NotNil(t, req.GetBody, "Seeker should enable GetBody")

		data, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		assert.Equal(t, "seekable", string(data))

		// Replay.
		replay, err := req.GetBody()
		require.NoError(t, err)
		replayData, err := io.ReadAll(replay)
		require.NoError(t, err)
		assert.Equal(t, "seekable", string(replayData))
	})

	t.Run("statter without Seeker", func(t *testing.T) {
		enc := httpc.BinaryEncoder("text/plain")
		body := &statOnlyFile{
			ReadCloser: io.NopCloser(strings.NewReader("statable")),
			size:       8,
		}

		req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
		require.NoError(t, err)
		err = enc.Encode(req, body)
		require.NoError(t, err)

		assert.Equal(t, int64(8), req.ContentLength, "should set Content-Length from Stat()")
		assert.Nil(t, req.GetBody, "no Seeker means no GetBody")
	})
}

func TestBinaryEncoderWithReplay(t *testing.T) {
	enc := httpc.BinaryEncoderWithReplay("text/plain")
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)

	factory := func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("replayable")), nil
	}
	err = enc.Encode(req, factory)
	require.NoError(t, err)

	assert.Equal(t, "text/plain", req.Header.Get("Content-Type"))
	assert.Equal(t, int64(-1), req.ContentLength)
	require.NotNil(t, req.GetBody, "BinaryEncoderWithReplay should set GetBody for retryability")

	// Read initial body.
	data, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.Equal(t, "replayable", string(data))

	// Replay via GetBody.
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayData, err := io.ReadAll(replay)
	require.NoError(t, err)
	assert.Equal(t, "replayable", string(replayData))
}

// --- Test helpers for BinaryEncoder fs.File/io.Seeker tests ---

// createTempFile creates a temporary file with the given content and returns it open for reading.
func createTempFile(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "codec-test-*")
	require.NoError(t, err)
	_, err = f.WriteString(content)
	require.NoError(t, err)
	_, err = f.Seek(0, io.SeekStart)
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// readSeekCloser implements io.ReadSeekCloser but NOT fs.File (no Stat method).
type readSeekCloser struct {
	*bytes.Reader
}

func newReadSeekCloser(data []byte) *readSeekCloser {
	return &readSeekCloser{Reader: bytes.NewReader(data)}
}

func (r *readSeekCloser) Close() error { return nil }

// statOnlyFile implements io.ReadCloser + Stat() but NOT io.Seeker.
type statOnlyFile struct {
	io.ReadCloser
	size int64
}

func (s *statOnlyFile) Stat() (fs.FileInfo, error) {
	return &fakeFileInfo{size: s.size}, nil
}

// fakeFileInfo is a minimal fs.FileInfo for testing.
type fakeFileInfo struct {
	size int64
}

func (fi *fakeFileInfo) Name() string       { return "fake" }
func (fi *fakeFileInfo) Size() int64        { return fi.size }
func (fi *fakeFileInfo) Mode() fs.FileMode  { return 0 }
func (fi *fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *fakeFileInfo) IsDir() bool        { return false }
func (fi *fakeFileInfo) Sys() any           { return nil }

// trackingBody wraps an io.ReadCloser and records whether Close was called.
type trackingBody struct {
	io.ReadCloser
	closed bool
}

func newTrackingBody(s string) *trackingBody {
	return &trackingBody{ReadCloser: io.NopCloser(strings.NewReader(s))}
}

func (tb *trackingBody) Close() error {
	tb.closed = true
	return tb.ReadCloser.Close()
}

func TestDecoders_CloseBody(t *testing.T) {
	t.Run("VoidDecoder closes body", func(t *testing.T) {
		body := newTrackingBody("discard me")
		resp := &http.Response{StatusCode: http.StatusOK, Body: body}
		_, err := httpc.VoidDecoder().Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.True(t, body.closed, "VoidDecoder should close the body")
	})

	t.Run("OptionalJSONDecoder closes body on 204", func(t *testing.T) {
		body := newTrackingBody("")
		resp := &http.Response{StatusCode: http.StatusNoContent, Body: body}
		result, err := httpc.OptionalJSONDecoder[testPayload]().Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.True(t, body.closed, "OptionalJSONDecoder should close body on no-content")
	})

	t.Run("OptionalBinaryDecoder closes body on 204", func(t *testing.T) {
		body := newTrackingBody("")
		resp := &http.Response{StatusCode: http.StatusNoContent, Body: body}
		result, err := httpc.OptionalBinaryDecoder().Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.Nil(t, result)
		assert.True(t, body.closed, "OptionalBinaryDecoder should close body on no-content")
	})

	t.Run("BinaryDecoder does not close body", func(t *testing.T) {
		body := newTrackingBody("caller owns this")
		resp := &http.Response{StatusCode: http.StatusOK, Body: body}
		result, err := httpc.BinaryDecoder().Decode(context.Background(), resp)
		require.NoError(t, err)
		assert.False(t, body.closed, "BinaryDecoder hands ownership to caller, should not close")
		_ = result.Close()
		assert.True(t, body.closed, "caller close should propagate")
	})

	t.Run("OptionalBinaryDecoder does not close body with content", func(t *testing.T) {
		body := newTrackingBody("caller owns this")
		resp := &http.Response{StatusCode: http.StatusOK, ContentLength: 16, Body: body}
		result, err := httpc.OptionalBinaryDecoder().Decode(context.Background(), resp)
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.False(t, body.closed, "OptionalBinaryDecoder hands ownership to caller when there is content")
		_ = result.Close()
		assert.True(t, body.closed, "caller close should propagate")
	})
}

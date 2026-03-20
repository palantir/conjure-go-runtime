package httpc_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

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

	t.Run("zero content length", func(t *testing.T) {
		resp := &http.Response{
			StatusCode:    http.StatusOK,
			ContentLength: 0,
			Body:          io.NopCloser(strings.NewReader("")),
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

func TestJSONEncoderWithPool(t *testing.T) {
	pool := newTrackingPool()
	enc := httpc.JSONEncoderWithPool[testPayload](pool)
	req, err := http.NewRequest(http.MethodPost, "http://example.com", nil)
	require.NoError(t, err)
	err = enc.Encode(req, testPayload{Name: "foo", Value: 42})
	require.NoError(t, err)

	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, 1, pool.gets)
	assert.Equal(t, 0, pool.puts, "buffer should not be returned before body is read")

	// Read body content.
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"foo","value":42}`, string(body))
	assert.Greater(t, req.ContentLength, int64(0))

	// Close returns buffer to pool.
	err = req.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, 1, pool.puts, "buffer should be returned to pool on Close")

	// Double close is safe.
	err = req.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, 1, pool.puts, "double close should not return buffer twice")

	// GetBody returns a replay reader (no pool buffer).
	require.NotNil(t, req.GetBody)
	replay, err := req.GetBody()
	require.NoError(t, err)
	replayBody, err := io.ReadAll(replay)
	require.NoError(t, err)
	assert.Equal(t, body, replayBody)
}

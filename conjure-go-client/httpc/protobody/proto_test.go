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

package protobody_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/protobody"
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/protobody/internal/testpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestEncoder_RoundTrip(t *testing.T) {
	enc := protobody.NewEncoder[*testpb.TestMessage]()
	req, err := http.NewRequest(http.MethodPost, "http://example.invalid/v1", nil)
	require.NoError(t, err)

	src := &testpb.TestMessage{Key: "hello", Value: "world"}
	require.NoError(t, enc.Encode(req, src))

	assert.Equal(t, "application/x-protobuf", req.Header.Get("Content-Type"))
	require.NotNil(t, req.Body)
	require.NotNil(t, req.GetBody, "Encoder must populate GetBody so retries can replay the body")
	assert.Positive(t, req.ContentLength)

	// Drain the body, then verify GetBody yields the same bytes for retry replay.
	body1, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	replay, err := req.GetBody()
	require.NoError(t, err)
	body2, err := io.ReadAll(replay)
	require.NoError(t, err)
	assert.Equal(t, body1, body2)

	// Decoded back through the proto runtime.
	var got testpb.TestMessage
	require.NoError(t, proto.Unmarshal(body1, &got))
	assert.Equal(t, src.Key, got.Key)
	assert.Equal(t, src.Value, got.Value)
}

func TestDecoder_RoundTrip(t *testing.T) {
	src := &testpb.TestMessage{Key: "k", Value: "v"}
	buf, err := proto.Marshal(src)
	require.NoError(t, err)
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader(buf))}

	dec := protobody.NewDecoder[testpb.TestMessage]()
	got, err := dec.Decode(context.Background(), resp)
	require.NoError(t, err)
	require.NotNil(t, got, "Decoder must allocate the concrete message, not return typed-nil")
	assert.Equal(t, "k", got.Key)
	assert.Equal(t, "v", got.Value)
}

func TestDecoder_InvalidProtoErrors(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(bytes.NewReader([]byte{0xff, 0xff, 0xff}))}

	dec := protobody.NewDecoder[testpb.TestMessage]()
	_, err := dec.Decode(context.Background(), resp)
	require.Error(t, err)
}

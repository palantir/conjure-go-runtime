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

// Package protobody provides httpc encoder and decoder implementations for
// protobuf-encoded request and response bodies. It is intentionally separate
// from the core httpc package so JSON-only callers do not pull in
// google.golang.org/protobuf transitively.
package protobody

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"google.golang.org/protobuf/proto"
)

const contentType = "application/x-protobuf"

// NewEncoder returns an httpc BodyEncoder that marshals values of T as
// protobuf and sets Content-Type: application/x-protobuf. The encoder
// populates GetBody so the request remains retryable.
func NewEncoder[T proto.Message]() *ProtoEncoder[T] {
	return &ProtoEncoder[T]{}
}

type ProtoEncoder[T proto.Message] struct{}

func (*ProtoEncoder[T]) Encode(req *http.Request, v T) error {
	buf, err := proto.Marshal(v)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Body = io.NopCloser(bytes.NewReader(buf))
	req.ContentLength = int64(len(buf))
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	return nil
}

// NewDecoder returns an httpc BodyDecoder that unmarshals the response body
// into a freshly allocated *M (returned as T). The two type parameters
// (M, T = *M) let Decode construct a concrete message without a factory
// function; Go can infer T from M at the call site:
//
//	dec := protobody.NewDecoder[MyMessage]()
//	// dec.Decode(...) returns (*MyMessage, error)
func NewDecoder[M any, T interface {
	*M
	proto.Message
}]() *ProtoDecoder[M, T] {
	return &ProtoDecoder[M, T]{}
}

type ProtoDecoder[M any, T interface {
	*M
	proto.Message
}] struct{}

func (*ProtoDecoder[M, T]) Decode(_ context.Context, resp *http.Response) (T, error) {
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	msg := T(new(M))
	if err := proto.Unmarshal(buf, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

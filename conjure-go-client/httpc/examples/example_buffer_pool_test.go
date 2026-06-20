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

package examples_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/bytesbuffers"
)

// Example_bufferPool reuses encoding buffers across requests.
//
// WithBufferPool gives the JSON encoder a pool to draw scratch buffers from instead of
// allocating one per request — worth it on hot paths. Conjure-generated clients set
// this from endpoint tags. Here a counting pool shows the encoder borrowing a buffer
// for each of three calls.
func Example_bufferPool() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	var gets atomic.Int32
	pool := &countingPool{inner: bytesbuffers.NewSizedPool(4, 1024), gets: &gets}

	// Endpoints are values; declare them once and reuse across calls.
	var (
		createItem = httpc.NewJSONPOST[bufferPoolItem, struct{}]("CreateItem", "/items").
			WithBufferPool(pool)
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	for range 3 {
		if _, _, err = createItem.WithBody(bufferPoolItem{Name: "widget"}).Execute(ctx, client); err != nil {
			panic(err)
		}
	}
	fmt.Println("buffers borrowed:", gets.Load())
	// Output:
	// buffers borrowed: 3
}

type bufferPoolItem struct {
	Name string `json:"name"`
}

// countingPool wraps a bytesbuffers.Pool and counts how often a buffer is borrowed.
type countingPool struct {
	inner bytesbuffers.Pool
	gets  *atomic.Int32
}

func (p *countingPool) Get() *bytes.Buffer {
	p.gets.Add(1)
	return p.inner.Get()
}

func (p *countingPool) Put(buf *bytes.Buffer) { p.inner.Put(buf) }

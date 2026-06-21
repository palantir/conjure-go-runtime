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
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_transportTuning tunes connection pooling.
//
// SetMaxIdleConns, SetMaxIdleConnsPerHost, and SetIdleConnTimeout size and age
// the pool of warm connections kept for reuse; SetKeepAlive sets the TCP
// keep-alive period. DisableKeepAlives forces a fresh connection per request,
// and DisableHTTP2 / SetHTTP2ReadIdleTimeout / SetHTTP2PingTimeout govern HTTP/2.
// Here three sequential requests reuse a single pooled connection.
func Example_transportTuning() {
	ctx := context.Background()
	var newConns atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxIdleConns(100).
		SetMaxIdleConnsPerHost(10).
		SetIdleConnTimeout(30 * time.Second).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	const requests = 3
	for range requests {
		if _, _, err = ping.Call().Execute(ctx, client); err != nil {
			panic(err)
		}
	}
	fmt.Println("requests:", requests)
	fmt.Println("new connections:", newConns.Load())
	// Output:
	// requests: 3
	// new connections: 1
}

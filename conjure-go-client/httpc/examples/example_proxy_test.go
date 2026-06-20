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
	"net/http"
	"net/http/httptest"
	"sync/atomic"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_proxy routes requests through an HTTP proxy.
//
// SetHTTPProxyURL sends every request through the given proxy; SetSocksProxyURL
// does the same for a SOCKS5 proxy, and SetProxyFromEnvironment honors
// HTTP_PROXY / HTTPS_PROXY / NO_PROXY. SetNoProxy clears all of these. Here the
// proxy forwards on behalf of the client, so the origin host is never contacted
// directly.
func Example_proxy() {
	ctx := context.Background()
	var viaProxy atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		viaProxy.Add(1)
		fmt.Println("proxy forwarding to:", r.Host)
	}))
	defer proxy.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs("http://inventory.example").
		SetHTTPProxyURL(proxy.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Execute(ctx, client); err != nil {
		panic(err)
	}
	fmt.Println("requests via proxy:", viaProxy.Load())
	// Output:
	// proxy forwarding to: inventory.example
	// requests via proxy: 1
}

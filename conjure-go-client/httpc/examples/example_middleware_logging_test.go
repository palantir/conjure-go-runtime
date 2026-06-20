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
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_middlewareLogging wraps every request with a custom middleware.
//
// A Middleware sees the outgoing request and the response (or transport error) and
// calls next.RoundTrip to continue the chain. This is the extension point for audit
// logging, request signing, header injection, and similar cross-cutting concerns.
// AddMiddleware runs it on every request the client makes; a real implementation
// would write to a structured logger rather than stdout.
func Example_middlewareLogging() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"widget"}`)
	}))
	defer server.Close()

	logging := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		fmt.Printf("--> %s %s\n", req.Method, req.URL.Path)
		resp, err := next.RoundTrip(req)
		if err != nil {
			fmt.Printf("<-- %s %s: %v\n", req.Method, req.URL.Path, err)
			return resp, err
		}
		fmt.Printf("<-- %s %s: %d\n", req.Method, req.URL.Path, resp.StatusCode)
		return resp, err
	})

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewJSONGET[loggingItem]("GetItem", "/items/widget")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		AddMiddleware(logging).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	item, _, err := getItem.Execute(ctx, client)
	if err != nil {
		panic(err)
	}

	fmt.Println("got:", item.Name)
	// Output:
	// --> GET /items/widget
	// <-- GET /items/widget: 200
	// got: widget
}

type loggingItem struct {
	Name string `json:"name"`
}

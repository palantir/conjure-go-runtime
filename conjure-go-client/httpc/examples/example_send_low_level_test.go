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

// Example_sendLowLevel sends a hand-built request through the full runtime
// without an endpoint descriptor.
//
// Send is the loop Call.Execute is built on: it runs the URL selector,
// retries, and middleware around a path-only *http.Request and returns the raw
// response. Unlike Execute it does not decode the body or apply an error decoder,
// so the caller handles the response directly. SendOptions carries per-request
// decoration, middleware, and call-policy overrides; the zero value applies the
// client's defaults.
func Example_sendLowLevel() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"widget"}`)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// The request URL is path-only; Send prepends the base URL the selector picks.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/items/widget", nil)
	if err != nil {
		panic(err)
	}

	resp, err := client.Send(ctx, req, httpc.SendOptions{})
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}
	fmt.Println("status:", resp.StatusCode)
	fmt.Printf("body: %s\n", body)
	// Output:
	// status: 200
	// body: {"name":"widget"}
}

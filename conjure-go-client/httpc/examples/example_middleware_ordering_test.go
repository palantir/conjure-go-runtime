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

// Example_middlewareOrdering shows where each kind of middleware sits in the stack.
//
// Layers run outermost-first: telemetry, then AddMiddleware (outer), then
// AddInnerMiddleware (inner), then the auth/header decoration, then a per-request
// WithMiddleware, then the transport. Only middleware below the decoration — here the
// per-request one — observes the Authorization header the client adds.
func Example_middlewareOrdering() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("server sees auth:", r.Header.Get("Authorization") != "")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	probe := func(label string) httpc.Middleware {
		return httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			fmt.Printf("%s sees auth: %t\n", label, req.Header.Get("Authorization") != "")
			return next.RoundTrip(req)
		})
	}

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewJSONGET[struct{}]("Ping", "/ping")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetAuthToken("secret-token").
		AddMiddleware(probe("outer")).
		AddInnerMiddleware(probe("inner")).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.WithMiddleware(probe("per-request")).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// outer sees auth: false
	// inner sees auth: false
	// per-request sees auth: true
	// server sees auth: true
}

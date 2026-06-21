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

// Example_basicAuth configures HTTP basic auth, including a provider that may skip it.
//
// SetBasicAuth sends static credentials on every request. SetBasicAuthOptionalProvider
// runs per request and may return nil to leave a request unauthenticated — useful when
// auth is conditional on request context. The server reads credentials with
// http.Request.BasicAuth.
func Example_basicAuth() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, ok := r.BasicAuth()
		fmt.Printf("ok=%t user=%q\n", ok, user)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	authed, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetBasicAuth("svc-account", "hunter2").
		Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Call().Execute(ctx, authed); err != nil {
		panic(err)
	}

	optional, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetBasicAuthOptionalProvider(func(context.Context) (*httpc.BasicAuth, error) {
			return nil, nil // nil skips auth, leaving Authorization unset
		}).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Call().Execute(ctx, optional); err != nil {
		panic(err)
	}
	// Output:
	// ok=true user="svc-account"
	// ok=false user=""
}

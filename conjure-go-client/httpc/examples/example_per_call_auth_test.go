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

// Example_perCallAuthorization overrides the client's auth for a single call.
//
// The client carries a default bearer token, sent on every call. A per-call
// WithAuthorization takes precedence over it: pass another Authorizer to act under
// a different identity (on-behalf-of) for one request, or NoAuthorization to send
// no credentials at all. NoAuthorization suppresses the client token rather than
// falling through to it — the right move for a public endpoint on an otherwise
// authenticated client.
func Example_perCallAuthorization() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			auth = "(none)"
		}
		fmt.Println("Authorization:", auth)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetAuth(httpc.BearerToken("client-token")).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Default: the client-level token is sent.
	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}

	// Per call: act on behalf of another identity, overriding the client token.
	if _, _, err = ping.Call().
		WithAuthorization(httpc.BearerToken("on-behalf-of-alice")).
		Execute(ctx, client); err != nil {
		panic(err)
	}

	// Per call: suppress auth entirely for a public endpoint.
	if _, _, err = ping.Call().
		WithAuthorization(httpc.NoAuthorization()).
		Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// Authorization: Bearer client-token
	// Authorization: Bearer on-behalf-of-alice
	// Authorization: (none)
}

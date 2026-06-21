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

// Example_authPrecedence shows that an explicit per-request Authorization
// supersedes the client's auth provider — and the provider never runs.
//
// Auth resolves by precedence, not by a runtime "set if absent" check. A per-request
// Authorization (here via WithHeader; WithAuthorization behaves the same) wins over the
// client's configured provider, so the provider is not consulted at all: no token is
// fetched and, crucially, a failing provider cannot fail a request that was going to
// override it anyway.
func Example_authPrecedence() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("server saw:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON().
			WithHeader("Authorization", "Bearer explicit-token")
	)

	providerRan := false
	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		// This provider would fail any request that actually consulted it.
		SetAuth(httpc.BearerTokenProvider(func(context.Context) (string, error) {
			providerRan = true
			return "", fmt.Errorf("token service unavailable")
		})).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}

	fmt.Println("auth provider ran:", providerRan)
	// Output:
	// server saw: Bearer explicit-token
	// auth provider ran: false
}

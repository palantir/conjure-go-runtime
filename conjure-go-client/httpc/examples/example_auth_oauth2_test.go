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
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/oauth2auth"
	"golang.org/x/oauth2"
)

// Example_oauth2TokenSource drives httpc auth from a golang.org/x/oauth2.TokenSource.
//
// oauth2auth.TokenSource (in the httpc/oauth2auth sub-package, so golang.org/x/oauth2
// stays out of core httpc) adapts any TokenSource to an httpc.Authorizer. Build the
// source however you like — client credentials, google.DefaultTokenSource, a cached
// oauth2.ReuseTokenSource — and hand the adapter to SetAuth. It emits the token's full
// "<Type> <AccessToken>" header, so non-Bearer schemes work too.
func Example_oauth2TokenSource() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Authorization:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: "ya29.token", TokenType: "Bearer"})
	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetAuth(oauth2auth.TokenSource(src)).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// Authorization: Bearer ya29.token
}

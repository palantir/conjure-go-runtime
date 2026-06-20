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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_buildHTTPClient builds a plain *http.Client to hand to a third-party SDK.
//
// Many SDKs accept only a *http.Client. BuildHTTPClient yields one whose transport
// still carries the builder's auth, metrics, and tracing — so requests made through
// the SDK remain authenticated and observable without any extra wiring. The result
// is refreshable so config changes (e.g. a rotated token) propagate; call Current to
// get the live *http.Client.
func Example_buildHTTPClient() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("server saw:", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	refreshableClient, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetAuthToken("secret-token").
		BuildHTTPClient(ctx)
	if err != nil {
		panic(err)
	}

	httpClient := refreshableClient.Current()
	resp, err := httpClient.Get(server.URL)
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()

	fmt.Println("status:", resp.StatusCode)
	// Output:
	// server saw: Bearer secret-token
	// status: 200
}

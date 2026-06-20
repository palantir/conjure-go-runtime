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

// Example_bearerToken attaches a bearer token via a provider.
//
// SetAuthTokenProvider is consulted on every request, so it can hand back a freshly
// minted or rotated token — here a new value per call. For a fixed token use
// SetAuthToken; for one that changes out of band use SetAuthTokenRefreshable. All
// three send "Authorization: Bearer <token>".
func Example_bearerToken() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Authorization:", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewJSONGET[struct{}]("Ping", "/ping")
	)

	issued := 0
	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetAuthTokenProvider(func(context.Context) (string, error) {
			issued++
			return fmt.Sprintf("token-%d", issued), nil
		}).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	for range 2 {
		if _, _, err = ping.Execute(ctx, client); err != nil {
			panic(err)
		}
	}
	// Output:
	// Authorization: Bearer token-1
	// Authorization: Bearer token-2
}

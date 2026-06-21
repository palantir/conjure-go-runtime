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
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_headers sets headers at the builder and endpoint layers and shows precedence.
//
// SetHeader/AddHeader on the builder apply to every request; WithHeader/WithAddedHeader
// on an endpoint apply per request. Headers resolve by precedence: for a given key a
// per-request set wins over a builder set (here X-Tenant), while adds from every layer
// accumulate (here X-Trace-Tags).
func Example_headers() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("X-Tenant:", r.Header.Get("X-Tenant"))
		fmt.Println("X-Trace-Tags:", strings.Join(r.Header.Values("X-Trace-Tags"), ", "))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewGET[struct{}]("GetItem", "/items/widget").WithJSON().
			WithHeader("X-Tenant", "acme").                // beats the builder's SetHeader
			WithAddedHeader("X-Trace-Tags", "team=search") // adds to the builder's value
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetHeader("X-Tenant", "default").      // every request, unless overridden
		AddHeader("X-Trace-Tags", "env=prod"). // every request, accumulates
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = getItem.Call().Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// X-Tenant: acme
	// X-Trace-Tags: env=prod, team=search
}

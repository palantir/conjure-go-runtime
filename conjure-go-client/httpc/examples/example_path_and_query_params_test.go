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

// Example_pathAndQueryParams fills a path template and adds query parameters.
//
// WithPathParam replaces a {name} placeholder (the value is URL-escaped). WithQuery
// sets a single-valued parameter; WithAddedQuery appends, so a key can carry multiple
// values. Query parameters resolve onto each attempt's request, so a retry never
// duplicates them.
func Example_pathAndQueryParams() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("path:", r.URL.Path)
		fmt.Println("query:", r.URL.Query().Encode())
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		search = httpc.NewGET[struct{}]("Search", "/orgs/{orgId}/items").WithJSON().
			WithQuery("limit", "10").
			WithAddedQuery("tag", "red").
			WithAddedQuery("tag", "round")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = search.Call().WithPathParam("orgId", "acme").Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// path: /orgs/acme/items
	// query: limit=10&tag=red&tag=round
}

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

// Example_perCallOverrides composes an endpoint's static defaults with per-call
// configuration.
//
// Configuration set on the descriptor is a static default baked into the RPC
// shape — here an Accept-Language header sent by every call. Values that vary per
// invocation live in an httpc.Overrides bag, typically derived from the call-site
// context (a tenant, a request-scoped id) and merged via Call.WithOverrides. The
// two layers compose: headers and query parameters from both are sent, so the
// shared descriptor is declared once and each call adds only what differs.
func Example_perCallOverrides() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Accept-Language:", r.Header.Get("Accept-Language"))
		fmt.Println("X-Tenant:", r.Header.Get("X-Tenant"))
		fmt.Println("page:", r.URL.Query().Get("page"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls. The
	// Accept-Language header is a static default for every call to this RPC.
	var (
		listItems = httpc.NewGET[struct{}]("ListItems", "/items").
			WithJSON().
			WithAddedHeader("Accept-Language", "en")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Per call, derive the values that vary — here the caller's tenant — and merge
	// them over the descriptor defaults.
	for _, tenant := range []string{"acme", "globex"} {
		perCall := httpc.Overrides{}.
			WithHeader("X-Tenant", tenant).
			WithQuery("page", "1")
		if _, _, err = listItems.Call().WithOverrides(perCall).Execute(ctx, client); err != nil {
			panic(err)
		}
	}
	// Output:
	// Accept-Language: en
	// X-Tenant: acme
	// page: 1
	// Accept-Language: en
	// X-Tenant: globex
	// page: 1
}

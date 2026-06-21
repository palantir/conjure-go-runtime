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

// Example_basicGet builds a minimal client and executes a typed JSON GET.
//
// A client is three calls on the builder: a service name (used for metrics and
// log tags), one or more base URLs, and Build. The endpoint is declared once with
// its response type; NewJSONGET wires a JSON decoder and Accept: application/json.
func Example_basicGet() {
	ctx := context.Background()
	// Stand-in for the remote service. In production this is a real host.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"widget","price":42}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewGET[basicGetItem]("GetItem", "/items/{id}").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	item, _, err := getItem.Call().WithPathParam("id", "widget").Execute(ctx, client)
	if err != nil {
		panic(err)
	}

	fmt.Printf("%s costs %d\n", item.Name, item.Price)
	// Output: widget costs 42
}

type basicGetItem struct {
	Name  string `json:"name"`
	Price int    `json:"price"`
}

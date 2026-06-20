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

// Example_migrationFromHTTPClient maps the legacy httpclient API onto httpc.
//
// The legacy conjure-go-client/httpclient package configured a client with
// functional ClientParams and issued each request with RequestParams on a single
// Do call. httpc splits these: builder setters configure the client once, and a
// typed Endpoint describes each request. The tables pair each httpc call with the
// httpclient predecessor it replaces.
//
//	httpclient ClientParam                httpc Builder
//	----------------------------------    ----------------------------------
//	WithServiceName("inventory")          SetServiceName("inventory")
//	WithBaseURLs([]string{url})           SetBaseURLs(url)
//	WithAuthToken(token)                  SetAuthToken(token)
//	WithHTTPTimeout(d)                    SetTimeout(d)
//	WithMaxRetries(n)                     SetMaxAttempts(new(n + 1))
//	WithConfig(cfg)                       ApplyConfig(ctx, cfg)
//
//	httpclient RequestParam               httpc Endpoint
//	----------------------------------    ----------------------------------
//	WithRequestMethod(GET) + WithPath     NewJSONGET[T]("Op", "/path")
//	WithJSONResponse(&out)                type parameter T (decoded result)
//	WithJSONRequest(in)                   NewJSONPOST[I, O] + WithBody(in)
//	WithPathf("/items/%s", id)            path built by the endpoint
//	WithRequestTimeout(d)                 Endpoint.WithTimeout(d)
func Example_migrationFromHTTPClient() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"widget"}`)
	}))
	defer server.Close()

	// Legacy:
	//   client, err := httpclient.NewClient(
	//       httpclient.WithServiceName("inventory"),
	//       httpclient.WithBaseURLs([]string{server.URL}),
	//       httpclient.WithMaxRetries(3),
	//   )
	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(4)).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Legacy:
	//   var item migrationItem
	//   _, err = client.Do(ctx,
	//       httpclient.WithRequestMethod(http.MethodGet),
	//       httpclient.WithPathf("/items/%s", "widget"),
	//       httpclient.WithJSONResponse(&item),
	//   )
	//
	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewJSONGET[migrationItem]("GetItem", "/items/widget")
	)

	item, _, err := getItem.Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Println("name:", item.Name)
	// Output:
	// name: widget
}

type migrationItem struct {
	Name string `json:"name"`
}

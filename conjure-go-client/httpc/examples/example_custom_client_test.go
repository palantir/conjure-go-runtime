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
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_customClient drives an Endpoint against a hand-written httpc.Client.
//
// httpc.Client is a small interface — Transport, Middleware, URLSelector, and
// CallPolicy. Implementing it directly lets an Endpoint run without the builder,
// which is handy for tests that serve canned responses from memory with no
// httptest server. A custom Client carries no builder auth, telemetry, or
// headers: it contributes only what these four methods return.
func Example_customClient() {
	ctx := context.Background()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewJSONGET[customClientItem]("GetItem", "/items/widget")
	)

	item, _, err := getItem.Execute(ctx, customClient{})
	if err != nil {
		panic(err)
	}
	fmt.Println("name:", item.Name)
	// Output:
	// name: widget
}

type customClientItem struct {
	Name string `json:"name"`
}

// customClient implements httpc.Client, serving every request from memory.
type customClient struct{}

func (customClient) Transport() http.RoundTripper { return cannedTransport{} }

func (customClient) Middleware() httpc.Middleware { return nil }

func (customClient) URLSelector() httpc.URLSelector {
	return httpc.BalancedURLSelector([]string{"https://inventory.example"})
}

func (customClient) CallPolicy() httpc.CallPolicy { return httpc.CallPolicy{} }

// cannedTransport answers every request with the same JSON item.
type cannedTransport struct{}

func (cannedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"name":"widget"}`)),
		Request:    req,
	}, nil
}

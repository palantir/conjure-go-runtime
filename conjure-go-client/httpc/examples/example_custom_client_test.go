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

// Example_customClient drives a typed call against a hand-written httpc.Runtime.
//
// httpc.Runtime is a one-method interface — Send. Implementing it directly lets a
// call run without the builder, which is handy for tests that serve canned
// responses from memory with no httptest server. A custom runtime owns the whole
// send: it gets no builder auth, telemetry, or retries unless it adds them.
func Example_customClient() {
	ctx := context.Background()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewGET[customClientItem]("GetItem", "/items/widget").WithJSON()
	)

	item, _, err := getItem.Call().Execute(ctx, customClient{})
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

// customClient implements httpc.Runtime, serving every request from memory.
type customClient struct{}

func (customClient) Send(_ context.Context, req *http.Request, _ httpc.SendOptions) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"name":"widget"}`)),
		Request:    req,
	}, nil
}

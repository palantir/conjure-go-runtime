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

// Example_errorDecoding reads the status code back from a failed request.
//
// With no decoder configured, Execute applies DefaultErrorDecoder: any response with
// status >= 307 becomes a Go error carrying the status code. StatusCodeFromError reads
// it back; for a 3xx redirect, LocationFromError returns the Location header. To get the
// raw response instead, set NoErrorDecoder (see Example_inspectRawErrors).
func Example_errorDecoding() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no such item", http.StatusNotFound)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewJSONGET[struct{}]("GetItem", "/items/widget")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	_, _, err = getItem.Execute(ctx, client)
	if err == nil {
		panic("expected an error")
	}

	if code, ok := httpc.StatusCodeFromError(err); ok {
		fmt.Println("status code:", code)
	}
	// Output:
	// status code: 404
}

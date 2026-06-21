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

// Example_inspectRawErrors bypasses error decoding to read a non-2xx body directly.
//
// WithNoErrorDecoder turns off the default status >= 307 handling, so Execute returns
// the raw response for every status code with a nil error. Pair it with a BinaryDecoder
// so the body is handed back intact for inspection; VoidDecoder would discard it. The
// caller is responsible for closing the returned reader.
func Example_inspectRawErrors() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "tenant quota exceeded")
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewGET[io.ReadCloser]("GetItem", "/items/widget").
			WithDecoder(httpc.BinaryDecoder()).
			WithNoErrorDecoder()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	body, resp, err := getItem.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	defer func() { _ = body.Close() }()

	raw, err := io.ReadAll(body)
	if err != nil {
		panic(err)
	}
	fmt.Println("status:", resp.StatusCode)
	fmt.Printf("body: %s\n", raw)
	// Output:
	// status: 403
	// body: tenant quota exceeded
}

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
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_compressedBody gzip-compresses the request body.
//
// GZIPEncoder wraps any inner encoder, compressing its output and setting
// Content-Encoding: gzip (SnappyEncoder and ZLIBEncoder are the other variants). The
// server is responsible for decompressing based on the Content-Encoding header.
func Example_compressedBody() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("content-encoding:", r.Header.Get("Content-Encoding"))
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(gz)
		fmt.Printf("decoded: %s\n", body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		createItem = httpc.NewPOST[compressedItem, struct{}]("CreateItem", "/items").WithJSON().
			WithEncoder(httpc.GZIPEncoder(httpc.JSONEncoder[compressedItem]()))
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = createItem.Call(compressedItem{Name: "widget"}).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// content-encoding: gzip
	// decoded: {"name":"widget"}
}

type compressedItem struct {
	Name string `json:"name"`
}

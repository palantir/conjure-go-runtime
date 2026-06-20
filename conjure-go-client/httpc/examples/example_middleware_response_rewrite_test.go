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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_middlewareResponseRewrite rewrites the response body before it is decoded.
//
// A middleware sees the response on the way back, so it can read the upstream body,
// transform it, and replace resp.Body. The endpoint's decoder then sees the rewritten
// bytes. Remember to close the original body and reset ContentLength to match.
func Example_middlewareResponseRewrite() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"widget"}`)
	}))
	defer server.Close()

	annotate := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		resp, err := next.RoundTrip(req)
		if err != nil {
			return resp, err
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return resp, err
		}
		var item map[string]any
		if err := json.Unmarshal(body, &item); err != nil {
			return resp, err
		}
		item["rewritten"] = true // add a field the upstream never sent
		rewritten, err := json.Marshal(item)
		if err != nil {
			return resp, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(rewritten))
		resp.ContentLength = int64(len(rewritten))
		return resp, nil
	})

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewJSONGET[rewriteItem]("GetItem", "/items/widget")
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		AddMiddleware(annotate).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	item, _, err := getItem.Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("name=%s rewritten=%t\n", item.Name, item.Rewritten)
	// Output:
	// name=widget rewritten=true
}

type rewriteItem struct {
	Name      string `json:"name"`
	Rewritten bool   `json:"rewritten"`
}

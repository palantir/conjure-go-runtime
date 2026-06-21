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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_postJSON sends a JSON request body and decodes a JSON response.
//
// NewPOST().WithJSON() wires a JSON encoder for the request type and a JSON decoder
// for the response type, and sets Accept: application/json. Call supplies the value to
// encode; Content-Type: application/json is set automatically.
func Example_postJSON() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in postJSONRequest
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(postJSONResponse{ID: "item-1", Name: in.Name})
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		createItem = httpc.NewPOST[postJSONRequest, postJSONResponse]("CreateItem", "/items").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	resp, _, err := createItem.
		Call(postJSONRequest{Name: "widget"}).
		Execute(ctx, client)
	if err != nil {
		panic(err)
	}

	fmt.Printf("created %s named %s\n", resp.ID, resp.Name)
	// Output: created item-1 named widget
}

type postJSONRequest struct {
	Name string `json:"name"`
}

type postJSONResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

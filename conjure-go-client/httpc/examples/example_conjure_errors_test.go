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
	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/errors"
)

// Example_conjureErrors decodes a Conjure structured error from a failed request.
//
// DefaultErrorDecoder unmarshals the standard Conjure error types automatically, so
// GetConjureError can pull the typed error out of the wrapped chain and branch on its
// code, name, or parameters. For custom error types, register a decoder with
// httpc.WithConjureErrorDecoder (or httpc.DefaultErrorDecoderWithConjure).
func Example_conjureErrors() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		errors.WriteErrorResponse(w, errors.NewNotFound())
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getItem = httpc.NewGET[struct{}]("GetItem", "/items/widget").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	_, _, err = getItem.Call().Execute(ctx, client)
	if err == nil {
		panic("expected an error")
	}

	if conjureErr := errors.GetConjureError(err); conjureErr != nil {
		fmt.Println("code:", conjureErr.Code())
		fmt.Println("name:", conjureErr.Name())
	}
	// Output:
	// code: NOT_FOUND
	// name: Default:NotFound
}

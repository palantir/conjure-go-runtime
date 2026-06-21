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

// Example_rebuildableClient reconfigures a client through its seeded builder.
//
// Build returns a RebuildableRuntime whose Builder method hands back a *Builder
// pre-populated (cloned) from the client's current configuration. Tweaking one
// setting and calling Build again yields a new client that inherits everything
// else — here the second client keeps the base URL and service name and changes
// only a header.
func Example_rebuildableClient() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("tier:", r.Header.Get("X-Tier"))
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetHeader("X-Tier", "standard").
		Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Execute(ctx, client); err != nil {
		panic(err)
	}

	// Reuse the existing configuration, overriding only the tier header.
	premium, err := client.Builder().SetHeader("X-Tier", "premium").Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Execute(ctx, premium); err != nil {
		panic(err)
	}
	// Output:
	// tier: standard
	// tier: premium
}

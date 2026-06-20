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
	"gopkg.in/yaml.v2"
)

// Example_configFromYAML builds a client from YAML configuration.
//
// ServicesConfig unmarshals a YAML document with a shared default plus
// per-service overrides; ApplyServicesConfig merges the two for one service and
// applies every field it sets (URIs, auth, timeouts, retries, TLS). Here the
// api-token from config becomes the Authorization header on every request.
func Example_configFromYAML() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("Authorization:", r.Header.Get("Authorization"))
	}))
	defer server.Close()

	// In production this YAML comes from a config file; here the test server's
	// address is spliced in to keep the example self-contained.
	configYAML := fmt.Sprintf(`
services:
  inventory:
    uris:
      - %s
    api-token: secret-token
    max-num-retries: 3
`, server.URL)

	var services httpc.ServicesConfig
	if err := yaml.Unmarshal([]byte(configYAML), &services); err != nil {
		panic(err)
	}

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		ApplyServicesConfig(ctx, services, "inventory").
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// Authorization: Bearer secret-token
}

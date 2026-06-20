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

// Example_tlsCertificates trusts a custom CA.
//
// CA configuration is additive: AddCACerts (and AddCACertFiles / AddCACertBytes)
// extend the trusted root pool, which by default still includes the system CAs —
// call SetIncludeSystemCAs(false) to trust only what you add. For mutual TLS,
// present a client certificate with SetClientCertFiles / SetClientCertBytes, and
// SetDynamicCertReload(true) to pick up rotated files. Here a self-signed test
// server is rejected until its certificate is trusted as a root.
func Example_tlsCertificates() {
	ctx := context.Background()
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	// Without trusting the server's CA, certificate verification fails.
	untrusting, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(1)).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	_, _, err = ping.Execute(ctx, untrusting)
	fmt.Println("untrusted CA, failed:", err != nil)

	// Trusting the server's self-signed certificate as a root CA succeeds.
	trusting, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		AddCACerts(server.Certificate()).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	_, _, err = ping.Execute(ctx, trusting)
	fmt.Println("trusted CA, failed:", err != nil)
	// Output:
	// untrusted CA, failed: true
	// trusted CA, failed: false
}

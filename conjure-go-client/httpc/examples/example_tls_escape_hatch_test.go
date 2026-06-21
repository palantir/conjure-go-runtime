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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_tlsEscapeHatch installs a caller-managed *tls.Config.
//
// SetTLSConfig hands the client a tls.Config to use verbatim, bypassing the
// builder's CA, client-cert, and InsecureSkipVerify construction entirely. It is
// the escape hatch for TLS settings the typed setters don't cover. The trade-off:
// the builder's own TLS setters no longer apply — as shown below, calling
// SetInsecureSkipVerify alongside SetTLSConfig has no effect, because the
// caller's config owns that field.
func Example_tlsEscapeHatch() {
	ctx := context.Background()
	server := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	// A caller-managed tls.Config that trusts the server's certificate.
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	_, _, err = ping.Call().Execute(ctx, client)
	fmt.Println("escape-hatch config, failed:", err != nil)

	// SetTLSConfig wins outright: SetInsecureSkipVerify is ignored, so an empty
	// escape-hatch config still rejects the untrusted server.
	ignored, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}).
		SetInsecureSkipVerify(true).
		SetMaxAttempts(new(1)).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	_, _, err = ping.Call().Execute(ctx, ignored)
	fmt.Println("skip-verify ignored under escape hatch, failed:", err != nil)
	// Output:
	// escape-hatch config, failed: false
	// skip-verify ignored under escape hatch, failed: true
}

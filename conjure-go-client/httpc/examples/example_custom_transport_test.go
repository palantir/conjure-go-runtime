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

// Example_customTransport injects a custom http.RoundTripper.
//
// SetTransport replaces the base transport the builder would otherwise construct
// from the dialer and TLS config; it runs beneath every middleware, on every
// attempt, making it the place for connection-level behavior or a test double.
// SetTransport wins over SetDialer. For request-scoped logic that needs the
// context (auth, logging), prefer a Middleware instead.
func Example_customTransport() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("X-Via:", r.Header.Get("X-Via"))
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetTransport(taggingTransport{base: http.DefaultTransport}).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// X-Via: custom-transport
}

// taggingTransport tags every outgoing request, then delegates to its base.
type taggingTransport struct {
	base http.RoundTripper
}

func (t taggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("X-Via", "custom-transport")
	return t.base.RoundTrip(req)
}

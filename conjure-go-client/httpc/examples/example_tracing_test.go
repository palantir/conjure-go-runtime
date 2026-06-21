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

// Example_tracing propagates a B3 trace header.
//
// By default the client starts a client span per request and injects B3 trace
// headers (X-B3-TraceId, X-B3-SpanId) from the ambient witchcraft trace span, so
// a downstream service joins the same trace. WithTraceHeader sets the trace ID
// explicitly for one request, as shown here. DisableTracing stops the client
// span and trace metrics; DisableTraceHeaderPropagation keeps the span but omits
// the outbound headers. There is no first-class OpenTelemetry integration — to
// propagate an OTel context, inject it from a custom middleware (see
// Example_middlewareRequestSigning for the middleware shape).
func Example_tracing() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		fmt.Println("X-B3-TraceId:", r.Header.Get("X-B3-TraceId"))
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = httpc.WithTraceHeader(ping, "4bf92f3577b34da6").Call().Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// X-B3-TraceId: 4bf92f3577b34da6
}

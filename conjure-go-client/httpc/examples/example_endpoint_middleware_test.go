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

type endpointMiddlewareResp struct {
	Tag string `json:"tag"`
}

// Example_endpointMiddleware bakes a middleware into a single RPC's descriptor, so it
// runs for that endpoint only — not the whole client.
//
// Unlike a client-wide middleware (Builder.AddMiddleware) or a static header, a
// descriptor middleware is scoped to one RPC and runs per attempt with the resolved
// request, so it can set values computed at send time. Here it stamps a header derived
// from the outgoing method and path; a second endpoint that does not opt in sends none.
func Example_endpointMiddleware() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"tag":%q}`, r.Header.Get("X-Endpoint-Tag"))
	}))
	defer server.Close()

	// Baked into one RPC's definition: stamps a header computed from the resolved
	// request. Only endpoints that opt in carry it.
	tagMiddleware := httpc.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		req.Header.Set("X-Endpoint-Tag", req.Method+" "+req.URL.Path)
		return next.RoundTrip(req)
	})

	var (
		report = httpc.NewGET[endpointMiddlewareResp]("Report", "/report").WithJSON().
			WithMiddleware(tagMiddleware)
		plain = httpc.NewGET[endpointMiddlewareResp]("Plain", "/plain").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	report1, _, err := report.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	plain1, _, err := plain.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("report endpoint tag: %q\n", report1.Tag)
	fmt.Printf("plain endpoint tag:  %q\n", plain1.Tag)
	// Output:
	// report endpoint tag: "GET /report"
	// plain endpoint tag:  ""
}

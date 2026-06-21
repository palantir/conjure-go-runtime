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

// Example_sendRequestValues drives a hand-built request through Runtime.Send with
// an explicit SendOptions: RequestValues carries per-call header, query, and
// basic-auth decoration (resolved per attempt, above the client's intrinsic
// values), and CallPolicyOverrides tweaks the call policy for this send only —
// here capping it at a single attempt. This is the same decoration Endpoint
// produces, expressed directly for callers using Send without an Endpoint.
func Example_sendRequestValues() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, _, _ := r.BasicAuth()
		fmt.Println("tenant:", r.Header.Get("X-Tenant"))
		fmt.Println("query:", r.URL.Query().Get("q"))
		fmt.Println("user:", user)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// The request URL is path-only; Send prepends the base URL the selector picks.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/items", nil)
	if err != nil {
		panic(err)
	}

	opts := httpc.SendOptions{
		Values: httpc.RequestValues{}.
			WithHeader("X-Tenant", "acme").
			WithQuery("q", "widget").
			WithBasicAuth("svc", "secret"),
		Policy: httpc.CallPolicyOverrides{}.WithMaxAttempts(new(1)),
	}
	resp, err := client.Send(ctx, req, opts)
	if err != nil {
		panic(err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Output:
	// tenant: acme
	// query: widget
	// user: svc
}

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
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_endpointDefaultTimeout bakes a per-attempt timeout into a single slow RPC as a
// descriptor static default, so every call site inherits it without repeating it.
//
// A descriptor's With* methods set defaults that are part of the RPC's definition. A
// known-slow endpoint (a report or bulk export) can carry its own generous per-attempt
// timeout while the rest of the client keeps a tighter default. The same client serves
// both endpoints below; only the descriptor default differs. A per-call WithTimeout
// still wins for one invocation.
func Example_endpointDefaultTimeout() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	var (
		quick  = httpc.NewGET[struct{}]("Quick", "/quick").WithJSON()
		report = httpc.NewGET[struct{}]("Report", "/report").WithJSON().
			WithTimeout(2 * time.Second) // slow RPC: generous per-attempt default
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetTimeout(5 * time.Millisecond). // too tight for the 30ms handler
		SetMaxAttempts(new(1)).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Quick inherits the 5ms client timeout and trips; Report uses its own 2s default.
	_, _, err = quick.Call().Execute(ctx, client)
	fmt.Println("quick (inherits 5ms client timeout), failed:", err != nil)

	_, _, err = report.Call().Execute(ctx, client)
	fmt.Println("report (2s endpoint default), failed:", err != nil)

	// A per-call timeout still overrides the descriptor default for one invocation.
	_, _, err = report.Call().WithTimeout(2*time.Millisecond).Execute(ctx, client)
	fmt.Println("report with per-call 2ms override, failed:", err != nil)
	// Output:
	// quick (inherits 5ms client timeout), failed: true
	// report (2s endpoint default), failed: false
	// report with per-call 2ms override, failed: true
}

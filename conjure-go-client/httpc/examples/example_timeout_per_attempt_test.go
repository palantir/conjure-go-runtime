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
	"sync/atomic"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_timeoutPerAttempt contrasts a per-attempt timeout with a whole-call deadline.
//
// WithTimeout (and SetTimeout) bounds a single attempt and resets on every retry, so it
// is not a budget for the whole call: with retries enabled the client still makes each
// attempt. To bound the entire operation across retries, put a deadline on the context
// passed to Execute — that is what stops an otherwise-unlimited retry loop.
func Example_timeoutPerAttempt() {
	ctx := context.Background()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	// A generous per-attempt timeout does not cap the number of attempts: all three
	// retries run and each sees the 503.
	perAttempt, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(3)).
		SetInitialBackoff(time.Millisecond).
		SetMaxBackoff(time.Millisecond).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Call().WithTimeout(time.Second).Execute(ctx, perAttempt); err == nil {
		panic("expected an error")
	}
	fmt.Println("attempts under per-attempt timeout:", attempts.Load())

	// With unlimited attempts against a failing server, the context deadline — not
	// the per-attempt timeout — is what ends the call.
	unlimited, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(0)).
		SetInitialBackoff(20 * time.Millisecond).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	deadlineCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, _, err = ping.Call().Execute(deadlineCtx, unlimited)
	fmt.Println("context deadline ended the call:", err != nil)
	// Output:
	// attempts under per-attempt timeout: 3
	// context deadline ended the call: true
}

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
	"sync/atomic"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_retriesAndBackoff retries a flaky server until it succeeds.
//
// The client retries idempotent requests that fail with a connection error or a QoS
// status (429/503) and backs off between attempts. SetMaxAttempts caps the total: nil
// keeps the default (2 per base URL), new(0) means unlimited, new(n) means exactly n.
// The backoff bounds are set small here so the example runs fast.
func Example_retriesAndBackoff() {
	ctx := context.Background()
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			http.Error(w, "warming up", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(4)).
		SetInitialBackoff(time.Millisecond).
		SetMaxBackoff(time.Millisecond).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}
	fmt.Println("server attempts:", attempts.Load())
	// Output:
	// server attempts: 3
}

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

// Example_timeouts sets a per-attempt timeout and overrides it for one request.
//
// SetTimeout bounds each attempt; the timer resets on retry. A per-request WithTimeout
// (equivalently Overrides.WithTimeout) overrides it for a single call. Here a generous
// client timeout lets the slow handler finish, while a tight per-request timeout trips.
func Example_timeouts() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(40 * time.Millisecond)
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
		SetTimeout(time.Second).
		SetMaxAttempts(new(1)).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	_, _, err = ping.Call().Execute(ctx, client)
	fmt.Println("generous timeout, failed:", err != nil)

	_, _, err = ping.Call().WithTimeout(5*time.Millisecond).Execute(ctx, client)
	fmt.Println("tight timeout, failed:", err != nil)
	// Output:
	// generous timeout, failed: false
	// tight timeout, failed: true
}

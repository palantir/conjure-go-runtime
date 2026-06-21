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

// Example_runtimeWrapper wraps a built client's httpc.Runtime to observe the whole
// Send — retries and URL selection included.
//
// httpc.Runtime is a one-method interface, so a wrapper can decorate Send and
// delegate to the inner runtime. Unlike a Middleware, which runs once per attempt
// inside the retry loop, a Runtime wrapper sits outside the loop: it runs once per
// logical call no matter how many attempts it takes. Here the server fails twice
// before succeeding, so it receives three requests while the wrapper's Send runs
// just once.
func Example_runtimeWrapper() {
	ctx := context.Background()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if requests.Add(1) < 3 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
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
		SetMaxAttempts(new(3)).
		SetInitialBackoff(time.Millisecond).
		SetMaxBackoff(time.Millisecond).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	wrapped := &countingRuntime{inner: client}
	if _, _, err = ping.Call().Execute(ctx, wrapped); err != nil {
		panic(err)
	}

	fmt.Println("server requests:", requests.Load())
	fmt.Println("runtime wrapper sends:", wrapped.sends.Load())
	// Output:
	// server requests: 3
	// runtime wrapper sends: 1
}

// countingRuntime decorates an httpc.Runtime, counting each whole-call Send before
// delegating to the inner runtime.
type countingRuntime struct {
	inner httpc.Runtime
	sends atomic.Int32
}

func (c *countingRuntime) Send(ctx context.Context, req *http.Request, opts httpc.SendOptions) (*http.Response, error) {
	c.sends.Add(1)
	return c.inner.Send(ctx, req, opts)
}

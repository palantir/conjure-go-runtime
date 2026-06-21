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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_urlSelection spreads requests across base URLs and fails over.
//
// With several base URLs the client load-balances and fails over: a 503 from one host
// is retried against another, so every request here still succeeds despite one dead
// host. BalancedURLSelector (the default) routes away from slow or erroring hosts;
// RandomURLSelector orders them uniformly at random. Pass either to SetURLSelector.
func Example_urlSelection() {
	ctx := context.Background()
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer down.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer healthy.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(down.URL, healthy.URL).
		SetURLSelector(httpc.BalancedURLSelector).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	successes := 0
	for range 5 {
		if _, _, execErr := ping.Call().Execute(ctx, client); execErr == nil {
			successes++
		}
	}
	fmt.Printf("%d/5 requests succeeded\n", successes)
	// Output:
	// 5/5 requests succeeded
}

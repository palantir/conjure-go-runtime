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

// Example_customURLSelector routes with a custom URLSelector.
//
// A URLSelector returns the base URLs in the order to try them and observes each
// attempt as a Middleware. The built-in BalancedURLSelector routes away from
// failing hosts; this strict-priority selector instead always tries the primary
// first and only falls back to the secondary — useful when one host is preferred.
func Example_customURLSelector() {
	ctx := context.Background()
	var primaryHits atomic.Int32
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		primaryHits.Add(1)
		http.Error(w, "primary down", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	secondary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer secondary.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(primary.URL, secondary.URL).
		SetURLSelector(func(uris []string) httpc.URLSelector { return prioritySelector{uris: uris} }).
		SetInitialBackoff(time.Millisecond).
		SetMaxBackoff(time.Millisecond).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	succeeded := 0
	for range 3 {
		if _, _, err = ping.Call().Execute(ctx, client); err == nil {
			succeeded++
		}
	}
	fmt.Println("requests succeeded:", succeeded)
	fmt.Println("primary attempts:", primaryHits.Load())
	// Output:
	// requests succeeded: 3
	// primary attempts: 3
}

// prioritySelector tries its base URLs in strict order, never reordering them.
type prioritySelector struct {
	uris []string
}

func (s prioritySelector) BaseURLs() []string { return s.uris }

func (s prioritySelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

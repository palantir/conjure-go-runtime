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

// Example_overrideRequestDefaults shows the per-call clear/default API: turning an
// inherited setting off, then restoring it back to the default.
//
// Each scalar override has an explicit "off" state and a "back to default" state.
// Here the descriptor's static default disables error decoding
// (WithNoErrorDecoder), so Execute hands back the raw response for any status
// instead of returning a decoded error; a single call restores the package
// DefaultErrorDecoder with WithDefaultErrorDecoder, so the 503 becomes an error
// again. Timeouts follow the same shape: WithUnlimitedTimeout turns the
// per-attempt timeout off and WithDefaultTimeout restores the client-level one.
func Example_overrideRequestDefaults() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// This RPC defaults to no error decoding: callers inspect the status themselves.
	var (
		health = httpc.NewGET[struct{}]("Health", "/health").
			WithJSON().
			WithNoErrorDecoder()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMaxAttempts(new(1)). // a 503 would otherwise be retried
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Descriptor default: error decoding is off, so a 503 is not an error.
	_, resp, err := health.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Printf("decoding disabled: status=%d err=%v\n", resp.StatusCode, err)

	// Restore the default decoder for one call: now the 503 is decoded as an error.
	_, _, err = health.Call().WithDefaultErrorDecoder().Execute(ctx, client)
	fmt.Println("decoding restored: gotError =", err != nil)
	// Output:
	// decoding disabled: status=503 err=<nil>
	// decoding restored: gotError = true
}

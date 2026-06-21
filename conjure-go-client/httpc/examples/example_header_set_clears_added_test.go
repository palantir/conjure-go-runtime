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
	"strings"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
)

// Example_headerSetClearsAdded shows how set and add resolve for the same key.
//
// Headers and query parameters resolve by precedence, not by mutation order. For one
// key, a set (WithHeader/WithQuery) is the single winner: it discards any earlier add
// for that key, including adds contributed by a lower layer when Overrides are merged.
// Adds for a key with no set accumulate in order. Here X-Mode is set after being added
// (only the set survives) while X-Tag is added twice (both survive).
func Example_headerSetClearsAdded() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("X-Mode:", strings.Join(r.Header.Values("X-Mode"), ", "))
		fmt.Println("X-Tag:", strings.Join(r.Header.Values("X-Tag"), ", "))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		getX = httpc.NewGET[struct{}]("Get", "/x").WithJSON()
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	overrides := httpc.Overrides{}.
		WithAddedHeader("X-Mode", "fast"). // discarded by the later set
		WithHeader("X-Mode", "safe").      // the single winner for X-Mode
		WithAddedHeader("X-Tag", "a").
		WithAddedHeader("X-Tag", "b") // both X-Tag adds accumulate

	if _, _, err = getX.Call().WithOverrides(overrides).Execute(ctx, client); err != nil {
		panic(err)
	}
	// Output:
	// X-Mode: safe
	// X-Tag: a, b
}

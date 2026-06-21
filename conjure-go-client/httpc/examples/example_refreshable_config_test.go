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
	"github.com/palantir/pkg/refreshable/v2"
)

// Example_refreshableConfig live-reloads configuration without rebuilding.
//
// ApplyConfigRefreshable wires the client to a refreshable ClientConfig: each
// request reads the current value, so updating the config (here the base URIs)
// redirects in-flight clients with no rebuild. This is how a long-lived process
// picks up config-file changes — base URIs, timeouts, retries, auth — at runtime.
func Example_refreshableConfig() {
	ctx := context.Background()
	primary := newWhereServer("primary")
	defer primary.Close()
	failover := newWhereServer("failover")
	defer failover.Close()

	config := refreshable.New(httpc.ClientConfig{
		ServiceName: "inventory",
		URIs:        []string{primary.URL},
	})

	client, err := httpc.NewBuilder().
		ApplyConfigRefreshable(ctx, config).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	// Endpoints are values; declare them once and reuse across calls.
	var (
		where = httpc.NewGET[whereItem]("Where", "/where").WithJSON()
	)

	item, _, err := where.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Println("before reload:", item.Server)

	// Swap the base URI; the existing client reads the new value on its next call.
	config.Update(httpc.ClientConfig{
		ServiceName: "inventory",
		URIs:        []string{failover.URL},
	})

	item, _, err = where.Call().Execute(ctx, client)
	if err != nil {
		panic(err)
	}
	fmt.Println("after reload:", item.Server)
	// Output:
	// before reload: primary
	// after reload: failover
}

type whereItem struct {
	Server string `json:"server"`
}

func newWhereServer(name string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"server":%q}`, name))
	}))
}

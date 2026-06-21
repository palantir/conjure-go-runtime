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

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/metrics"
)

// Example_metrics reads the client.response metric the client emits per request.
//
// Clients built with a service name automatically time every request as the
// client.response timer, tagged with method, status family, and RPC method name.
// The timer is recorded on the metrics registry in the request context, so a
// caller installs one with metrics.WithRegistry. SetMetrics adds custom tag
// providers (StaticTagsProvider here); SetDisableMetrics turns emission off.
func Example_metrics() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	defer server.Close()

	registry := metrics.NewRootMetricsRegistry()
	ctx = metrics.WithRegistry(ctx, registry)

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	client, err := httpc.NewBuilder().
		SetServiceName("inventory").
		SetBaseURLs(server.URL).
		SetMetrics(httpc.StaticTagsProvider(metrics.Tags{metrics.MustNewTag("endpoint", "ping")})).
		Build(ctx)
	if err != nil {
		panic(err)
	}

	if _, _, err = ping.Call().Execute(ctx, client); err != nil {
		panic(err)
	}

	var count int64
	var endpointTag string
	registry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
		if name == "client.response" {
			if c, ok := value.Value("count").(int64); ok {
				count = c
			}
			endpointTag = tags.ToMap()["endpoint"]
		}
	})
	fmt.Println("client.response count:", count)
	fmt.Println("endpoint tag:", endpointTag)
	// Output:
	// client.response count: 1
	// endpoint tag: ping
}

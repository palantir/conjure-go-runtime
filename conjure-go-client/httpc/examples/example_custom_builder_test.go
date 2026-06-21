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
)

// regionBuilder is a downstream builder that embeds httpc.BuilderCore to add its
// own field (region) while inheriting every base setter with leaf-typed chaining
// — no forwarding setters to hand-write. Because the core is generic over the
// leaf, the promoted base setters return *regionBuilder, so base and leaf setters
// interleave in one chain.
type regionBuilder struct {
	*httpc.BuilderCore[*regionBuilder]
	region string
}

// newRegionBuilder wires the embedded core to the leaf; this (or CloneCoreFor) is
// the only way to construct one, since the core's self field is unexported.
func newRegionBuilder() *regionBuilder {
	b := &regionBuilder{}
	b.BuilderCore = httpc.NewBuilderCore(b)
	return b
}

// SetRegion is a leaf-only setter. It returns *regionBuilder so it chains with
// the promoted base setters, and drives a base setter from its own field.
func (b *regionBuilder) SetRegion(region string) *regionBuilder {
	b.region = region
	b.SetHeader("X-Region", region)
	return b
}

// Clone copies the leaf field and rebinds the core to the new leaf via
// CloneCoreFor, so chaining off the clone returns the clone (not the original).
func (b *regionBuilder) Clone() *regionBuilder {
	c := &regionBuilder{region: b.region}
	c.BuilderCore = b.BuilderCore.CloneCoreFor(c)
	return c
}

// Example_customBuilder extends the builder with a custom field by embedding
// httpc.BuilderCore. The leaf gets leaf-typed chaining across base and custom
// setters, and Build's RebuildableRuntime hands the leaf back from Builder(), so
// the custom field survives a rebuild round-trip.
func Example_customBuilder() {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("region header:", r.Header.Get("X-Region"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Endpoints are values; declare them once and reuse across calls.
	var (
		ping = httpc.NewGET[struct{}]("Ping", "/ping").WithDecoder(httpc.VoidDecoder())
	)

	// Base setters (SetServiceName, SetBaseURLs) and the leaf setter (SetRegion)
	// chain together — all return *regionBuilder.
	client, err := newRegionBuilder().
		SetServiceName("inventory").
		SetRegion("us-east").
		SetBaseURLs(server.URL).
		Build(ctx)
	if err != nil {
		panic(err)
	}
	if _, _, err = ping.Execute(ctx, client); err != nil {
		panic(err)
	}

	// Builder() returns the *regionBuilder, so the custom field round-trips.
	fmt.Println("rebuilt region:", client.Builder().region)
	// Output:
	// region header: us-east
	// rebuilt region: us-east
}

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

package httpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regionBuilder is a downstream custom builder: it embeds the exported
// [httpc.BuilderCore] and adds its own field. Living in package httpc_test, it
// can only touch the exported surface — exactly what a real downstream sees — so
// these tests prove the embeddable-core contract works from outside the package.
type regionBuilder struct {
	*httpc.BuilderCore[*regionBuilder]
	region string
}

func newRegionBuilder() *regionBuilder {
	b := &regionBuilder{}
	b.BuilderCore = httpc.NewBuilderCore(b)
	return b
}

// SetRegion is a leaf-only setter. Returning *regionBuilder lets it interleave
// freely with the promoted base setters in a single chain.
func (b *regionBuilder) SetRegion(region string) *regionBuilder {
	b.region = region
	return b
}

func (b *regionBuilder) Clone() *regionBuilder {
	c := &regionBuilder{region: b.region}
	c.BuilderCore = b.BuilderCore.CloneCoreFor(c)
	return c
}

// The load-bearing assertion: a custom leaf satisfies BuilderAPI for its OWN
// type. This holds only if every base setter is promoted from the core and
// returns *regionBuilder — the default-leaf assertion var _ BuilderAPI[*Builder]
// would not catch a setter accidentally left bound to *Builder.
var _ httpc.BuilderAPI[*regionBuilder] = (*regionBuilder)(nil)

// TestCustomLeaf_ChainingReturnsLeaf proves base and leaf setters interleave in
// one chain — which compiles only because the promoted base setters return the
// leaf type — and that the chained builder still builds a client.
func TestCustomLeaf_ChainingReturnsLeaf(t *testing.T) {
	ctx := context.Background()
	b := newRegionBuilder().
		SetServiceName("svc").
		SetRegion("us-east").
		SetBaseURLs("https://example.com").
		SetTimeout(5 * time.Second).
		SetRegion("us-west")
	require.Equal(t, "us-west", b.region)

	client, err := b.Build(ctx)
	require.NoError(t, err)
	require.NotNil(t, client)
}

// TestCustomLeaf_CoreOnlyHelpersPromoted exercises the core methods that are not
// part of BuilderAPI (so no interface assertion covers them): ApplyConfig,
// ApplyConfigRefreshable, and the BuildHTTPClient escape hatch. Calling them on
// the leaf forces their promotion and confirms they return the leaf type.
func TestCustomLeaf_CoreOnlyHelpersPromoted(t *testing.T) {
	ctx := context.Background()
	cfg := httpc.ClientConfig{
		ServiceName: "svc",
		URIs:        []string{"https://example.com"},
	}

	b := newRegionBuilder().SetRegion("eu-west")
	require.Equal(t, "eu-west", b.ApplyConfig(ctx, cfg).region)
	require.Equal(t, "eu-west", b.ApplyConfigRefreshable(ctx, refreshable.New(cfg)).region)

	hc, err := b.BuildHTTPClient(ctx)
	require.NoError(t, err)
	require.NotNil(t, hc)
}

// TestCustomLeaf_FieldSurvivesRebuild proves the custom field round-trips through
// Build → Builder → SetX → Build: Builder() hands back the real leaf type, so a
// rebuilt client carries (and can mutate) the downstream field.
func TestCustomLeaf_FieldSurvivesRebuild(t *testing.T) {
	ctx := context.Background()
	client, err := newRegionBuilder().
		SetRegion("ap-south").
		SetBaseURLs("https://example.com").
		Build(ctx)
	require.NoError(t, err)

	rebuilt := client.Builder()
	assert.Equal(t, "ap-south", rebuilt.region)

	client2, err := rebuilt.SetRegion("ap-northeast").Build(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ap-northeast", client2.Builder().region)
}

// TestCustomLeaf_CloneRebindsSelf proves CloneCoreFor rebinds self to the new
// leaf: chaining a base setter off the clone returns the clone, not the source.
// Observable only externally, since self is unexported.
func TestCustomLeaf_CloneRebindsSelf(t *testing.T) {
	src := newRegionBuilder().SetRegion("r1")
	clone := src.Clone()

	assert.Equal(t, "r1", clone.region, "Clone must copy the custom leaf field")
	assert.NotSame(t, src, clone)
	assert.Same(t, clone, clone.SetServiceName("svc"),
		"a base setter chained off the clone must return the clone leaf, not the source")
}

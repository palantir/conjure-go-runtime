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

package httpclient_test

import (
	"context"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewClientFromRefreshableConfigSubscriberCleanup verifies that creating
// and discarding clients built from a refreshable config does not leak
// subscribers on the root config refreshable. Each call to
// NewClientFromRefreshableConfig creates a chain of derived refreshables
// (MapWithError → MapFromValidated fan-out → Map for transport/dialer/etc.)
// that subscribe to the root config. When the client is garbage collected,
// these subscriptions must be cleaned up via runtime.AddCleanup cascading
// through the derived wrappers.
//
// Without proper cleanup, each discarded client leaks one subscriber on the
// root config, leading to unbounded memory growth in production services that
// create clients dynamically (e.g. per-service import backends).
func TestNewClientFromRefreshableConfigSubscriberCleanup(t *testing.T) {
	cfg := httpclient.ClientConfig{
		ServiceName: "test-service",
		URIs:        []string{"https://localhost:8080"},
	}
	config := refreshable.New(cfg)

	// Warmup: create and discard one client to establish steady state.
	_, err := httpclient.NewClientFromRefreshableConfig(context.Background(), config)
	require.NoError(t, err)
	forceGCAndCleanup()

	baseline := updatableSubscriberCount(t, config)
	t.Logf("Baseline subscriber count after warmup: %d", baseline)

	// Create and discard 100 clients.
	const iterations = 100
	for range iterations {
		_, err := httpclient.NewClientFromRefreshableConfig(context.Background(), config)
		require.NoError(t, err)
	}

	beforeGC := updatableSubscriberCount(t, config)
	t.Logf("Subscriber count after %d clients (before GC): %d", iterations, beforeGC)

	forceGCAndCleanup()

	final := updatableSubscriberCount(t, config)
	growth := final - baseline
	t.Logf("Final subscriber count: %d (growth from baseline: %d)", final, growth)

	// Each client adds 1 subscriber to config via validatedFromRefreshable.
	// With proper GC cleanup, all subscribers should be removed. Allow a
	// small tolerance for in-flight GC cycles.
	assert.Less(t, growth, 10,
		"subscriber count grew by %d after discarding %d clients; "+
			"the cleanup chain from derived refreshables back to the root config is not working",
		growth, iterations)
}

// TestNewHTTPClientFromRefreshableConfigSubscriberCleanup is the same test
// as above but exercises the NewHTTPClientFromRefreshableConfig path which
// returns a refreshable.Refreshable[*http.Client] instead of a Client.
func TestNewHTTPClientFromRefreshableConfigSubscriberCleanup(t *testing.T) {
	cfg := httpclient.ClientConfig{
		ServiceName: "test-service",
		URIs:        []string{"https://localhost:8080"},
	}
	config := refreshable.New(cfg)

	// Warmup.
	_, err := httpclient.NewHTTPClientFromRefreshableConfig(context.Background(), config)
	require.NoError(t, err)
	forceGCAndCleanup()

	baseline := updatableSubscriberCount(t, config)
	t.Logf("Baseline subscriber count after warmup: %d", baseline)

	const iterations = 100
	for range iterations {
		_, err := httpclient.NewHTTPClientFromRefreshableConfig(context.Background(), config)
		require.NoError(t, err)
	}

	forceGCAndCleanup()

	final := updatableSubscriberCount(t, config)
	growth := final - baseline
	t.Logf("Final subscriber count: %d (growth from baseline: %d)", final, growth)

	assert.Less(t, growth, 10,
		"subscriber count grew by %d after discarding %d clients",
		growth, iterations)
}

// updatableSubscriberCount uses reflection to read the length of the internal
// subscribers slice on a defaultRefreshable (the concrete type behind Updatable).
func updatableSubscriberCount(t *testing.T, updatable any) int {
	t.Helper()
	v := reflect.ValueOf(updatable)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	subs := v.FieldByName("subscribers")
	require.True(t, subs.IsValid(), "could not find subscribers field via reflection")
	return subs.Len()
}

// forceGCAndCleanup runs multiple GC cycles with pauses to allow
// runtime.AddCleanup callbacks to execute and cascade through multi-level
// derived refreshable chains.
func forceGCAndCleanup() {
	for range 20 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
}

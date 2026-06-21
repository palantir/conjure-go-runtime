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

package httpc

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverrides_CallPolicyTimeoutStates exercises the three timeout states the
// unexported merge/callPolicyOverrides distinguish, resolved against a base
// policy timeout. Unset and "default" both inherit the base; unlimited and the
// WithTimeout(0) alias disable it; a custom value applies.
func TestOverrides_CallPolicyTimeoutStates(t *testing.T) {
	base := CallPolicy{Timeout: 30 * time.Second}
	timeoutOf := func(o Overrides) time.Duration {
		return o.callPolicyOverrides().applyTo(base).Timeout
	}

	assert.Equal(t, 30*time.Second, timeoutOf(Overrides{}), "unset inherits the base timeout")
	assert.Equal(t, 30*time.Second, timeoutOf(Overrides{}.WithDefaultTimeout()), "cleared inherits the base timeout")
	assert.Equal(t, time.Duration(0), timeoutOf(Overrides{}.WithUnlimitedTimeout()), "unlimited disables it")
	assert.Equal(t, time.Duration(0), timeoutOf(Overrides{}.WithTimeout(0)), "WithTimeout(0) stays an unlimited alias")
	assert.Equal(t, 5*time.Second, timeoutOf(Overrides{}.WithTimeout(5*time.Second)), "custom applies")
}

// TestOverrides_MergeClearIsSticky verifies an explicit clear set by o survives a
// later merge whose o leaves the field unset — i.e. "cleared" does not silently
// re-inherit the receiver's earlier value.
func TestOverrides_MergeClearIsSticky(t *testing.T) {
	base := CallPolicy{Timeout: 30 * time.Second}

	withTimeout := Overrides{}.WithTimeout(5 * time.Second)
	cleared := withTimeout.merge(Overrides{}.WithDefaultTimeout())
	again := cleared.merge(Overrides{}) // o leaves timeout unset

	assert.Equal(t, 5*time.Second, withTimeout.callPolicyOverrides().applyTo(base).Timeout)
	assert.Equal(t, 30*time.Second, cleared.callPolicyOverrides().applyTo(base).Timeout, "clear wins over the 5s default")
	assert.Equal(t, 30*time.Second, again.callPolicyOverrides().applyTo(base).Timeout, "clear stays after an unset merge")
}

// header resolves an Overrides' header contributors the way the runtime does.
func header(t *testing.T, o Overrides) http.Header {
	t.Helper()
	h, _, err := o.requestValues().Snapshot(context.Background())
	require.NoError(t, err)
	return h
}

// TestOverrides_MergeDefaultAuthorizationClears verifies merging a
// WithDefaultAuthorization clears the receiver's authorizer, while an unset o
// leaves it intact.
func TestOverrides_MergeDefaultAuthorizationClears(t *testing.T) {
	withAuth := Overrides{}.WithAuthorization(BasicCredentials("u", "p"))
	assert.Equal(t, basicAuthHeader("u", "p"), header(t, withAuth).Get("Authorization"))

	assert.Equal(t, basicAuthHeader("u", "p"), header(t, withAuth.merge(Overrides{})).Get("Authorization"),
		"unset o keeps the receiver's auth")
	assert.Empty(t, header(t, withAuth.merge(Overrides{}.WithDefaultAuthorization())).Get("Authorization"),
		"clear drops the receiver's auth")
}

// TestOverrides_HeaderPrecedence pins the set/add resolution after the move from
// maps to ordered RequestValues contributors: set-then-add accumulates, add-then-set
// replaces, and the WithAuthorization scalar beats an explicit Authorization header
// in either order (it is always emitted as the trailing contributor).
func TestOverrides_HeaderPrecedence(t *testing.T) {
	t.Run("set then add accumulates", func(t *testing.T) {
		o := Overrides{}.WithHeader("X-A", "1").WithAddedHeader("X-A", "2")
		assert.Equal(t, []string{"1", "2"}, header(t, o).Values("X-A"))
	})
	t.Run("add then set replaces", func(t *testing.T) {
		o := Overrides{}.WithAddedHeader("X-A", "1").WithHeader("X-A", "2")
		assert.Equal(t, []string{"2"}, header(t, o).Values("X-A"))
	})
	t.Run("authorizer beats an Authorization header set first", func(t *testing.T) {
		o := Overrides{}.WithHeader("Authorization", "Bearer x").WithAuthorization(BasicCredentials("u", "p"))
		assert.Equal(t, basicAuthHeader("u", "p"), header(t, o).Get("Authorization"))
	})
	t.Run("authorizer beats an Authorization header set last", func(t *testing.T) {
		o := Overrides{}.WithAuthorization(BasicCredentials("u", "p")).WithHeader("Authorization", "Bearer x")
		assert.Equal(t, basicAuthHeader("u", "p"), header(t, o).Get("Authorization"))
	})
}

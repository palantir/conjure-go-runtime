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

// TestCallPolicyOverrides_ApplyTo covers the set-bit semantics: an unset field
// leaves the base unchanged, while zero/nil are meaningful when explicitly set.
func TestCallPolicyOverrides_ApplyTo(t *testing.T) {
	base := callPolicy{
		Timeout:        60 * time.Second,
		MaxAttempts:    new(3),
		InitialBackoff: 250 * time.Millisecond,
		MaxBackoff:     2 * time.Second,
	}

	t.Run("zero overrides leave base unchanged", func(t *testing.T) {
		assert.Equal(t, base, CallPolicyOverrides{}.applyTo(base))
	})

	t.Run("explicit zero timeout disables the per-attempt timeout", func(t *testing.T) {
		got := CallPolicyOverrides{}.WithTimeout(0).applyTo(base)
		assert.Equal(t, time.Duration(0), got.Timeout)
		assert.Equal(t, base.MaxAttempts, got.MaxAttempts, "other fields untouched")
	})

	t.Run("max attempts distinguishes unset from explicit nil", func(t *testing.T) {
		assert.Equal(t, base.MaxAttempts, CallPolicyOverrides{}.applyTo(base).MaxAttempts, "unset keeps base")

		got := CallPolicyOverrides{}.WithMaxAttempts(nil).applyTo(base)
		assert.Nil(t, got.MaxAttempts, "explicit nil overrides to the default formula")

		got = CallPolicyOverrides{}.WithMaxAttempts(new(7)).applyTo(base)
		require.NotNil(t, got.MaxAttempts)
		assert.Equal(t, 7, *got.MaxAttempts)
	})

	t.Run("backoff overrides apply", func(t *testing.T) {
		got := CallPolicyOverrides{}.WithInitialBackoff(time.Second).WithMaxBackoff(5 * time.Second).applyTo(base)
		assert.Equal(t, time.Second, got.InitialBackoff)
		assert.Equal(t, 5*time.Second, got.MaxBackoff)
	})
}

// TestRequestValues_ConcatPrecedence verifies the public With* builders produce
// contributors that resolve with later-wins precedence, and that concat layers
// other's values after the receiver's (so per-call values win over intrinsic).
func TestRequestValues_ConcatPrecedence(t *testing.T) {
	assert.True(t, RequestValues{}.isEmpty())
	assert.False(t, RequestValues{}.WithHeader("X-A", "1").isEmpty())

	intrinsic := RequestValues{}.WithHeader("X-A", "intrinsic")
	perCall := RequestValues{}.WithHeader("X-A", "per-call").WithAddedHeader("X-B", "b")
	merged := intrinsic.concat(perCall)

	h := http.Header{}
	require.NoError(t, resolveValues(context.Background(), h, merged.headerValues...))
	assert.Equal(t, "per-call", h.Get("X-A"), "later (per-call) WithHeader replaces the earlier intrinsic value")
	assert.Equal(t, "b", h.Get("X-B"))

	// concat with an empty side returns the non-empty side unchanged.
	assert.Equal(t, intrinsic, intrinsic.concat(RequestValues{}))
	assert.Equal(t, perCall, RequestValues{}.concat(perCall))
}

// TestRequestValues_Snapshot covers the read-only snapshot exposed for fakes and
// custom runtimes: it resolves headers/query/authorization with the same precedence
// the runtime applies per attempt, without mutating anything.
func TestRequestValues_Snapshot(t *testing.T) {
	v := RequestValues{}.
		WithHeader("X-A", "1").
		WithAddedHeader("X-A", "2").
		WithQuery("q", "x").
		WithAuthorization(BasicCredentials("u", "p"))

	header, query, err := v.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"1", "2"}, header["X-A"], "set then add accumulates")
	assert.Equal(t, "x", query.Get("q"))
	assert.Equal(t, basicAuthHeader("u", "p"), header.Get("Authorization"))
}

// TestRequestValues_WithAuthorizationNilNoOp verifies a nil Authorizer is a no-op
// on RequestValues (no scalar/sentinel to clear) — an existing trailing authorizer
// still resolves through.
func TestRequestValues_WithAuthorizationNilNoOp(t *testing.T) {
	v := RequestValues{}.WithAuthorization(BasicCredentials("u", "p")).WithAuthorization(nil)
	header, _, err := v.Snapshot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, basicAuthHeader("u", "p"), header.Get("Authorization"), "nil is a no-op, prior authorizer survives")
}

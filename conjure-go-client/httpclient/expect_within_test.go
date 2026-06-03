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

package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/deadlines"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpectWithinMiddleware_NoContext(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Expect-Within header is set with proposed deadline based on timeout
		headerValue := r.Header.Get(deadlines.HeaderExpectWithin)
		assert.NotEmpty(t, headerValue)
		// The value should be approximately 60 seconds (the timeout value)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create middleware
	timeout := refreshable.New(60 * time.Second)
	enforcement := refreshable.New(deadlines.EnforcementDefer)
	middleware := newExpectWithinMiddleware(enforcement, timeout)

	// Create request without expect-within context
	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	// Execute request through middleware
	resp, err := middleware.RoundTrip(req, http.DefaultTransport)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestExpectWithinMiddleware_WithContext(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Expect-Within header is set
		headerValue := r.Header.Get(deadlines.HeaderExpectWithin)
		assert.NotEmpty(t, headerValue)
		// Header format is validated by deadlines package tests

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create middleware
	timeout := refreshable.New(60 * time.Second)
	enforcement := refreshable.New(deadlines.EnforcementDefer)
	middleware := newExpectWithinMiddleware(enforcement, timeout)

	// Create request with expect-within context
	ctx := context.Background()
	ewc := deadlines.ProvidedDeadline{
		Remaining: 5 * time.Second,
		StartTime: time.Now().UnixMilli(),
	}
	ctx = deadlines.ContextWithExpectWithin(ctx, ewc)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	// Execute request through middleware
	resp, err := middleware.RoundTrip(req, http.DefaultTransport)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestExpectWithinMiddleware_ExpiredDeadline(t *testing.T) {
	// Create middleware
	timeout := refreshable.New(60 * time.Second)
	enforcement := refreshable.New(deadlines.EnforcementDefer)
	middleware := newExpectWithinMiddleware(enforcement, timeout)

	// Create request with expired deadline
	ctx := context.Background()
	ewc := deadlines.ProvidedDeadline{
		Remaining:   100 * time.Millisecond,
		StartTime:   time.Now().UnixMilli() - 200, // Started 200ms ago
		Enforcement: deadlines.EnforcementEnforce, // Enforce the deadline
	}
	ctx = deadlines.ContextWithExpectWithin(ctx, ewc)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	require.NoError(t, err)

	// Execute request through middleware - should fail
	resp, err := middleware.RoundTrip(req, http.DefaultTransport)
	assert.Nil(t, resp)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, deadlines.ErrDeadlineExpiredExternal))
}

func TestExpectWithinMiddleware_Disabled(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Expect-Within header is set
		headerValue := r.Header.Get(deadlines.HeaderExpectWithin)
		assert.NotEmpty(t, headerValue)

		// Verify Expect-Within-Enforced header is set to "false"
		enforcedHeader := r.Header.Get(deadlines.HeaderExpectWithinEnforced)
		assert.Equal(t, "false", enforcedHeader)

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create middleware with enforcement disabled
	timeout := refreshable.New(60 * time.Second)
	enforcement := refreshable.New(deadlines.EnforcementDisable)
	middleware := newExpectWithinMiddleware(enforcement, timeout)

	// Create request with expect-within context
	ctx := context.Background()
	ewc := deadlines.ProvidedDeadline{
		Remaining:   5 * time.Second,
		StartTime:   time.Now().UnixMilli(),
		Enforcement: deadlines.EnforcementDefer,
	}
	ctx = deadlines.ContextWithExpectWithin(ctx, ewc)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)

	// Execute request through middleware - should send headers with enforcement disabled
	resp, err := middleware.RoundTrip(req, http.DefaultTransport)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestExpectWithinMiddleware_WithEnforcement(t *testing.T) {
	tests := []struct {
		name                        string
		enforcement                 deadlines.Enforcement
		expectedEnforcementHeader   string
		shouldHaveEnforcementHeader bool
	}{
		{
			name:                        "enforce",
			enforcement:                 deadlines.EnforcementEnforce,
			expectedEnforcementHeader:   "true",
			shouldHaveEnforcementHeader: true,
		},
		{
			name:                        "disable",
			enforcement:                 deadlines.EnforcementDisable,
			expectedEnforcementHeader:   "false",
			shouldHaveEnforcementHeader: true,
		},
		{
			name:                        "defer",
			enforcement:                 deadlines.EnforcementDefer,
			expectedEnforcementHeader:   "",
			shouldHaveEnforcementHeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify Expect-Within header is set
				headerValue := r.Header.Get(deadlines.HeaderExpectWithin)
				assert.NotEmpty(t, headerValue)

				// Verify Expect-Within-Enforced header
				enforcedHeader := r.Header.Get(deadlines.HeaderExpectWithinEnforced)
				if tt.shouldHaveEnforcementHeader {
					assert.Equal(t, tt.expectedEnforcementHeader, enforcedHeader)
				} else {
					assert.Empty(t, enforcedHeader)
				}

				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()

			// Create middleware with the test enforcement
			timeout := refreshable.New(60 * time.Second)
			enforcement := refreshable.New(tt.enforcement)
			middleware := newExpectWithinMiddleware(enforcement, timeout)

			// Create request with expect-within context
			ctx := context.Background()
			ewc := deadlines.ProvidedDeadline{
				Remaining:   5 * time.Second,
				StartTime:   time.Now().UnixMilli(),
				Enforcement: deadlines.EnforcementDefer, // Context enforcement
			}
			ctx = deadlines.ContextWithExpectWithin(ctx, ewc)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)

			// Execute request through middleware
			resp, err := middleware.RoundTrip(req, http.DefaultTransport)
			require.NoError(t, err)
			assert.Equal(t, http.StatusOK, resp.StatusCode)
		})
	}
}

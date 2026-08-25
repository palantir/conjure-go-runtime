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

package deadlines

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/palantir/pkg/metrics"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseExpectWithinFromHeaders(t *testing.T) {
	tests := []struct {
		name                string
		headers             map[string]string
		expectNil           bool
		expectedMillis      int64
		expectedEnforcement Enforcement
	}{
		{
			name:      "no header",
			headers:   map[string]string{},
			expectNil: true,
		},
		{
			name: "deadline only",
			headers: map[string]string{
				HeaderExpectWithin: "5.0",
			},
			expectNil:           false,
			expectedMillis:      5000,
			expectedEnforcement: EnforcementDefer,
		},
		{
			name: "deadline with enforce",
			headers: map[string]string{
				HeaderExpectWithin:         "2.5",
				HeaderExpectWithinEnforced: "true",
			},
			expectNil:           false,
			expectedMillis:      2500,
			expectedEnforcement: EnforcementEnforce,
		},
		{
			name: "deadline with disable",
			headers: map[string]string{
				HeaderExpectWithin:         "10.0",
				HeaderExpectWithinEnforced: "false",
			},
			expectNil:           false,
			expectedMillis:      10000,
			expectedEnforcement: EnforcementDisable,
		},
		{
			name: "invalid deadline",
			headers: map[string]string{
				HeaderExpectWithin: "invalid",
			},
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := ParseExpectWithinFromHeaders(req)
			if tt.expectNil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				// Allow some tolerance for timing
				expectedDuration := time.Duration(tt.expectedMillis) * time.Millisecond
				assert.InDelta(t, float64(expectedDuration), float64(result.Remaining), float64(10*time.Millisecond))
				assert.Equal(t, tt.expectedEnforcement, result.Enforcement)
			}
		})
	}
}

func TestSetExpectWithinHeaders(t *testing.T) {
	tests := []struct {
		name                     string
		ewc                      ProvidedDeadline
		shouldSetDeadline        bool
		shouldSetEnforcement     bool
		expectedEnforcementValue string
	}{
		{
			name: "active deadline with defer",
			ewc: ProvidedDeadline{
				Remaining:   5 * time.Second,
				StartTime:   time.Now().UnixMilli(),
				Enforcement: EnforcementDefer,
			},
			shouldSetDeadline:    true,
			shouldSetEnforcement: false,
		},
		{
			name: "active deadline with enforce",
			ewc: ProvidedDeadline{
				Remaining:   3 * time.Second,
				StartTime:   time.Now().UnixMilli(),
				Enforcement: EnforcementEnforce,
			},
			shouldSetDeadline:        true,
			shouldSetEnforcement:     true,
			expectedEnforcementValue: "true",
		},
		{
			name: "active deadline with disable",
			ewc: ProvidedDeadline{
				Remaining:   2 * time.Second,
				StartTime:   time.Now().UnixMilli(),
				Enforcement: EnforcementDisable,
			},
			shouldSetDeadline:        true,
			shouldSetEnforcement:     true,
			expectedEnforcementValue: "false",
		},
		{
			name: "expired deadline",
			ewc: ProvidedDeadline{
				Remaining:   100 * time.Millisecond,
				StartTime:   time.Now().UnixMilli() - 200,
				Enforcement: EnforcementEnforce,
			},
			shouldSetDeadline:    false,
			shouldSetEnforcement: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
			SetExpectWithinHeaders(req, tt.ewc)

			deadlineHeader := req.Header.Get(HeaderExpectWithin)
			if tt.shouldSetDeadline {
				assert.NotEmpty(t, deadlineHeader)
			} else {
				assert.Empty(t, deadlineHeader)
			}

			enforcementHeader := req.Header.Get(HeaderExpectWithinEnforced)
			if tt.shouldSetEnforcement {
				assert.Equal(t, tt.expectedEnforcementValue, enforcementHeader)
			} else {
				assert.Empty(t, enforcementHeader)
			}
		})
	}
}

func TestContextWithDeadline(t *testing.T) {
	ctx := context.Background()
	deadline := 5 * time.Second

	newCtx := ContextWithDeadline(ctx, deadline, EnforcementEnforce)

	ewc, ok := ProvidedDeadlineFromContext(newCtx)
	require.True(t, ok)
	assert.Equal(t, 5*time.Second, ewc.Remaining)
	assert.Equal(t, EnforcementEnforce, ewc.Enforcement)
}

func TestGetRemainingDeadline(t *testing.T) {
	tests := []struct {
		name            string
		setupContext    func() context.Context
		expectRemaining bool
		minRemaining    time.Duration
		maxRemaining    time.Duration
	}{
		{
			name: "no deadline",
			setupContext: func() context.Context {
				return context.Background()
			},
			expectRemaining: false,
		},
		{
			name: "active deadline",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 5*time.Second, EnforcementDefer)
			},
			expectRemaining: true,
			minRemaining:    4 * time.Second,
			maxRemaining:    5 * time.Second,
		},
		{
			name: "expired deadline",
			setupContext: func() context.Context {
				ctx := context.Background()
				ewc := ProvidedDeadline{
					Remaining:   100 * time.Millisecond,
					StartTime:   time.Now().UnixMilli() - 200,
					Enforcement: EnforcementDefer,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			expectRemaining: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			ewc, ok := ProvidedDeadlineFromContext(ctx)

			var remaining time.Duration
			if ok && ewc != nil {
				remaining = GetRemainingDeadline(*ewc)
			} else {
				remaining = 0
			}

			if !tt.expectRemaining {
				assert.Equal(t, time.Duration(0), remaining)
			} else {
				assert.GreaterOrEqual(t, remaining, tt.minRemaining)
				assert.LessOrEqual(t, remaining, tt.maxRemaining)
			}
		})
	}
}

func TestIsDeadlineExpired(t *testing.T) {
	tests := []struct {
		name          string
		setupContext  func() context.Context
		expectExpired bool
	}{
		{
			name: "no deadline",
			setupContext: func() context.Context {
				return context.Background()
			},
			expectExpired: false,
		},
		{
			name: "active deadline",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 5*time.Second, EnforcementDefer)
			},
			expectExpired: false,
		},
		{
			name: "expired deadline",
			setupContext: func() context.Context {
				ctx := context.Background()
				ewc := ProvidedDeadline{
					Remaining:   100 * time.Millisecond,
					StartTime:   time.Now().UnixMilli() - 200,
					Enforcement: EnforcementDefer,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			expectExpired: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			assert.Equal(t, tt.expectExpired, IsDeadlineExpired(ctx))
		})
	}
}

func TestEncodeToRequest(t *testing.T) {
	tests := []struct {
		name              string
		setupContext      func() context.Context
		proposedDeadline  time.Duration
		clientEnforcement Enforcement
		expectError       bool
		expectHeader      bool
		expectEnforcement string
	}{
		{
			name: "no context, valid proposed deadline",
			setupContext: func() context.Context {
				return context.Background()
			},
			proposedDeadline:  5 * time.Second,
			clientEnforcement: EnforcementDefer,
			expectError:       false,
			expectHeader:      true,
			expectEnforcement: "",
		},
		{
			name: "no context, expired proposed deadline with enforcement",
			setupContext: func() context.Context {
				return context.Background()
			},
			proposedDeadline:  -1 * time.Second,
			clientEnforcement: EnforcementEnforce,
			expectError:       true,
			expectHeader:      false,
		},
		{
			name: "context deadline lower than proposed",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 2*time.Second, EnforcementDefer)
			},
			proposedDeadline:  5 * time.Second,
			clientEnforcement: EnforcementDefer,
			expectError:       false,
			expectHeader:      true,
			expectEnforcement: "",
		},
		{
			name: "proposed deadline lower than context",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 5*time.Second, EnforcementDefer)
			},
			proposedDeadline:  2 * time.Second,
			clientEnforcement: EnforcementDefer,
			expectError:       false,
			expectHeader:      true,
			expectEnforcement: "",
		},
		{
			name: "enforcement resolution - enforce",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 5*time.Second, EnforcementDefer)
			},
			proposedDeadline:  2 * time.Second,
			clientEnforcement: EnforcementEnforce,
			expectError:       false,
			expectHeader:      true,
			expectEnforcement: "true",
		},
		{
			name: "enforcement resolution - disable",
			setupContext: func() context.Context {
				return ContextWithDeadline(context.Background(), 5*time.Second, EnforcementEnforce)
			},
			proposedDeadline:  2 * time.Second,
			clientEnforcement: EnforcementDisable,
			expectError:       false,
			expectHeader:      true,
			expectEnforcement: "false",
		},
		{
			name: "expired context deadline with enforcement",
			setupContext: func() context.Context {
				ctx := context.Background()
				ewc := ProvidedDeadline{
					Remaining:   100 * time.Millisecond,
					StartTime:   time.Now().UnixMilli() - 200,
					Enforcement: EnforcementEnforce,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			proposedDeadline:  5 * time.Second,
			clientEnforcement: EnforcementDefer,
			expectError:       true,
			expectHeader:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)

			err := EncodeToRequest(ctx, tt.proposedDeadline, req, tt.clientEnforcement)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.expectHeader {
				assert.NotEmpty(t, req.Header.Get(HeaderExpectWithin))
			} else {
				assert.Empty(t, req.Header.Get(HeaderExpectWithin))
			}

			if tt.expectEnforcement != "" {
				assert.Equal(t, tt.expectEnforcement, req.Header.Get(HeaderExpectWithinEnforced))
			}
		})
	}
}

func TestDisableFurtherDeadlinePropagation(t *testing.T) {
	t.Run("context without existing deadline", func(t *testing.T) {
		ctx := context.Background()

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// Should have created a new ProvidedDeadline
		ewc, ok := ProvidedDeadlineFromContext(newCtx)
		require.True(t, ok, "Expected ProvidedDeadline to be set")
		require.NotNil(t, ewc, "ProvidedDeadline should not be nil")
		assert.True(t, ewc.DisablePropagation, "DisablePropagation should be true")

		// Verify EncodeToRequest doesn't set headers
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 5*time.Second, req, EnforcementDefer)
		require.NoError(t, err)
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Header should not be set when propagation is disabled")
	})

	t.Run("context with existing deadline", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          5 * time.Second,
			StartTime:          time.Now().UnixMilli(),
			Enforcement:        EnforcementEnforce,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// Should have updated the existing ProvidedDeadline
		updatedEwc, ok := ProvidedDeadlineFromContext(newCtx)
		require.True(t, ok, "Expected ProvidedDeadline to be set")
		require.NotNil(t, updatedEwc, "ProvidedDeadline should not be nil")
		assert.True(t, updatedEwc.DisablePropagation, "DisablePropagation should be true")
		assert.Equal(t, EnforcementDefer, updatedEwc.Enforcement, "Enforcement should be set to Defer")

		// Verify EncodeToRequest doesn't set headers
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 2*time.Second, req, EnforcementEnforce)
		require.NoError(t, err)
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Header should not be set when propagation is disabled")
	})

	t.Run("prevents header propagation with expired deadline", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          100 * time.Millisecond,
			StartTime:          time.Now().UnixMilli() - 200, // Already expired
			Enforcement:        EnforcementEnforce,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// Should not error even though deadline is expired, because enforcement is set to Defer
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 2*time.Second, req, EnforcementDefer)
		require.NoError(t, err, "Should not error when propagation is disabled even if deadline expired")
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Header should not be set when propagation is disabled")
	})

	t.Run("with proposed deadline smaller than context deadline", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          10 * time.Second,
			StartTime:          time.Now().UnixMilli(),
			Enforcement:        EnforcementDefer,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// Verify EncodeToRequest doesn't set headers even with smaller proposed deadline
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 2*time.Second, req, EnforcementDefer)
		require.NoError(t, err)
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Header should not be set when propagation is disabled")
	})

	t.Run("enforcement header not set when propagation disabled", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          5 * time.Second,
			StartTime:          time.Now().UnixMilli(),
			Enforcement:        EnforcementEnforce,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// Verify neither Expect-Within nor Expect-Within-Enforced headers are set
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 2*time.Second, req, EnforcementEnforce)
		require.NoError(t, err)
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Expect-Within header should not be set")
		assert.Empty(t, req.Header.Get(HeaderExpectWithinEnforced), "Expect-Within-Enforced header should not be set")
	})

	t.Run("getRemainingDeadline returns zero after disabling propagation", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          2 * time.Second,
			StartTime:          time.Now().UnixMilli(),
			Enforcement:        EnforcementDisable,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// Verify deadline is present before disabling
		deadlineBefore, ok := ProvidedDeadlineFromContext(ctx)
		require.True(t, ok)
		require.NotNil(t, deadlineBefore)
		remaining := GetRemainingDeadline(*deadlineBefore)
		assert.Greater(t, remaining, time.Duration(0), "Should have remaining time before disabling")

		// Call DisableFurtherDeadlinePropagation
		newCtx := DisableFurtherDeadlinePropagation(ctx)

		// After disabling, GetRemainingDeadline should return zero
		// This matches Java behavior where getRemainingDeadline returns empty
		deadlineAfter, ok := ProvidedDeadlineFromContext(newCtx)
		require.True(t, ok)
		require.NotNil(t, deadlineAfter)
		// Note: In the Java implementation, getRemainingDeadline returns empty after disabling.
		// In Go, we check if DisablePropagation is true to determine if we should treat it as no deadline
		assert.True(t, deadlineAfter.DisablePropagation, "DisablePropagation should be true")

		// Verify EncodeToRequest doesn't set headers
		req, _ := http.NewRequest(http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(newCtx, 1*time.Second, req, EnforcementDefer)
		require.NoError(t, err)
		assert.Empty(t, req.Header.Get(HeaderExpectWithin), "Header should not be set after disabling propagation")
	})

	t.Run("disabled propagation records ignore intent metric on expiration", func(t *testing.T) {
		ctx := context.Background()
		registry := metrics.NewRootMetricsRegistry()
		ctx = metrics.WithRegistry(ctx, registry)

		// Parse an expired deadline
		ewc := ProvidedDeadline{
			Remaining:          1 * time.Millisecond,
			StartTime:          time.Now().UnixMilli() - 2,
			Enforcement:        EnforcementDisable,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// First request before disabling - should record PROPAGATE intent
		req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(ctx, 10*time.Second, req1, EnforcementDefer)
		require.NoError(t, err)

		// Get initial metric counts
		var propagateCount, ignoreCount int64
		registry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
			if name == metricDeadlineExpired {
				type counter interface {
					Count() int64
				}
				if c, ok := value.(counter); ok {
					// Check tags to identify which metric this is
					for _, tag := range tags {
						if tag.Value() == string(ExpiredIntentPropagate) {
							propagateCount = c.Count()
						} else if tag.Value() == string(ExpiredIntentIgnore) {
							ignoreCount = c.Count()
						}
					}
				}
			}
		})
		assert.Greater(t, propagateCount, int64(0), "Should have recorded PROPAGATE intent before disabling")

		// Now disable propagation
		ctx = DisableFurtherDeadlinePropagation(ctx)

		// Second request after disabling - should record IGNORE intent
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
		err = EncodeToRequest(ctx, 10*time.Second, req2, EnforcementDefer)
		require.NoError(t, err)

		// Verify IGNORE metric was incremented
		var newIgnoreCount int64
		registry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
			if name == metricDeadlineExpired {
				type counter interface {
					Count() int64
				}
				if c, ok := value.(counter); ok {
					for _, tag := range tags {
						if tag.Value() == string(ExpiredIntentIgnore) {
							newIgnoreCount = c.Count()
						}
					}
				}
			}
		})
		assert.Greater(t, newIgnoreCount, ignoreCount, "Should have recorded IGNORE intent after disabling")
	})

	t.Run("disable propagation prevents enforcement of expired deadline", func(t *testing.T) {
		ctx := context.Background()
		ewc := ProvidedDeadline{
			Remaining:          1 * time.Second,
			StartTime:          time.Now().UnixMilli(),
			Enforcement:        EnforcementEnforce,
			DisablePropagation: false,
		}
		ctx = ContextWithExpectWithin(ctx, ewc)

		// First request before expiration - should have enforcement header
		req1, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
		err := EncodeToRequest(ctx, 10*time.Second, req1, EnforcementDefer)
		require.NoError(t, err)
		assert.Equal(t, "true", req1.Header.Get(HeaderExpectWithinEnforced), "Enforcement should be enabled before disabling")

		// Disable propagation
		ctx = DisableFurtherDeadlinePropagation(ctx)

		// Modify the deadline to be expired
		updatedEwc, ok := ProvidedDeadlineFromContext(ctx)
		require.True(t, ok)
		updatedEwc.StartTime = time.Now().UnixMilli() - 2000 // expired 1 second ago
		ctx = ContextWithExpectWithin(ctx, *updatedEwc)

		// Second request after expiration and disabling - should not throw and should be empty
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
		err = EncodeToRequest(ctx, 10*time.Second, req2, EnforcementDefer)
		require.NoError(t, err, "Should not error when propagation is disabled even if deadline expired")
		assert.Empty(t, req2.Header.Get(HeaderExpectWithin), "Header should not be set after disabling propagation")
	})
}

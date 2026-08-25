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

func TestBudgetBucket(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected ExpiredBudget
	}{
		{
			name:     "sub 100ms",
			duration: 50 * time.Millisecond,
			expected: ExpiredBudgetSub100ms,
		},
		{
			name:     "exactly 100ms",
			duration: 100 * time.Millisecond,
			expected: ExpiredBudgetSub1s,
		},
		{
			name:     "sub 1s",
			duration: 500 * time.Millisecond,
			expected: ExpiredBudgetSub1s,
		},
		{
			name:     "exactly 1s",
			duration: time.Second,
			expected: ExpiredBudgetSub10s,
		},
		{
			name:     "sub 10s",
			duration: 5 * time.Second,
			expected: ExpiredBudgetSub10s,
		},
		{
			name:     "exactly 10s",
			duration: 10 * time.Second,
			expected: ExpiredBudgetSub100s,
		},
		{
			name:     "sub 100s",
			duration: 50 * time.Second,
			expected: ExpiredBudgetSub100s,
		},
		{
			name:     "exactly 100s",
			duration: 100 * time.Second,
			expected: ExpiredBudgetAbove100s,
		},
		{
			name:     "above 100s",
			duration: 200 * time.Second,
			expected: ExpiredBudgetAbove100s,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := budgetBucket(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMetricsRecorded(t *testing.T) {
	tests := []struct {
		name               string
		setupContext       func() context.Context
		proposedDeadline   time.Duration
		clientEnforcement  Enforcement
		expectError        bool
		expectedCause      ExpiredCause
		expectedIntent     ExpiredIntent
		expectedBudget     ExpiredBudget
		expectMetricMarked bool
	}{
		{
			name: "no expiration - no metrics",
			setupContext: func() context.Context {
				registry := metrics.NewRootMetricsRegistry()
				return metrics.WithRegistry(context.Background(), registry)
			},
			proposedDeadline:   5 * time.Second,
			clientEnforcement:  EnforcementDefer,
			expectError:        false,
			expectMetricMarked: false,
		},
		{
			name: "expired proposed deadline with enforcement",
			setupContext: func() context.Context {
				registry := metrics.NewRootMetricsRegistry()
				return metrics.WithRegistry(context.Background(), registry)
			},
			proposedDeadline:   -1 * time.Second,
			clientEnforcement:  EnforcementEnforce,
			expectError:        true,
			expectedCause:      ExpiredCauseExternal,
			expectedIntent:     ExpiredIntentThrow,
			expectedBudget:     ExpiredBudgetSub1s,
			expectMetricMarked: true,
		},
		{
			name: "expired internal deadline with enforcement",
			setupContext: func() context.Context {
				registry := metrics.NewRootMetricsRegistry()
				ctx := metrics.WithRegistry(context.Background(), registry)
				ewc := ProvidedDeadline{
					Remaining:   100 * time.Millisecond,
					StartTime:   time.Now().UnixMilli() - 200,
					Enforcement: EnforcementEnforce,
					Internal:    true,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			proposedDeadline:   5 * time.Second,
			clientEnforcement:  EnforcementDefer,
			expectError:        true,
			expectedCause:      ExpiredCauseInternal,
			expectedIntent:     ExpiredIntentThrow,
			expectedBudget:     ExpiredBudgetSub100ms,
			expectMetricMarked: true,
		},
		{
			name: "expired deadline without enforcement - propagate",
			setupContext: func() context.Context {
				registry := metrics.NewRootMetricsRegistry()
				return metrics.WithRegistry(context.Background(), registry)
			},
			proposedDeadline:   -1 * time.Second,
			clientEnforcement:  EnforcementDefer,
			expectError:        false,
			expectedCause:      ExpiredCauseExternal,
			expectedIntent:     ExpiredIntentPropagate,
			expectedBudget:     ExpiredBudgetSub1s,
			expectMetricMarked: true,
		},
		{
			name: "expired already expired deadline - propagate-already-expired",
			setupContext: func() context.Context {
				registry := metrics.NewRootMetricsRegistry()
				ctx := metrics.WithRegistry(context.Background(), registry)
				ewc := ProvidedDeadline{
					Remaining:   -100 * time.Millisecond,
					StartTime:   time.Now().UnixMilli(),
					Enforcement: EnforcementDefer,
					Internal:    false,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			proposedDeadline:   5 * time.Second,
			clientEnforcement:  EnforcementDefer,
			expectError:        false,
			expectedCause:      ExpiredCauseExternal,
			expectedIntent:     ExpiredIntentPropagateAlreadyExpired,
			expectedBudget:     ExpiredBudgetSub100ms,
			expectMetricMarked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)

			err := EncodeToRequest(ctx, tt.proposedDeadline, req, tt.clientEnforcement)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.expectMetricMarked {
				registry := metrics.FromContext(ctx)

				// Use the Each method to find the metric
				found := false
				var foundCount int64
				registry.Each(func(name string, tags metrics.Tags, value metrics.MetricVal) {
					if name == metricDeadlineExpired {
						found = true
						// MetricVal interface should have Count() method
						type counter interface {
							Count() int64
						}
						if c, ok := value.(counter); ok {
							foundCount = c.Count()
						}
					}
				})

				require.True(t, found, "Expected to find deadline.expired metric")
				assert.Greater(t, foundCount, int64(0), "Expected metric to be marked")
			}
		})
	}
}

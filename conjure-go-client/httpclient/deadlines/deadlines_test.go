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
				assert.InDelta(t, tt.expectedMillis, result.RemainingMillis, 10)
				assert.Equal(t, tt.expectedEnforcement, result.Enforcement)
			}
		})
	}
}

func TestSetExpectWithinHeaders(t *testing.T) {
	tests := []struct {
		name                     string
		ewc                      ExpectWithinContext
		shouldSetDeadline        bool
		shouldSetEnforcement     bool
		expectedEnforcementValue string
	}{
		{
			name: "active deadline with defer",
			ewc: ExpectWithinContext{
				RemainingMillis: 5000,
				StartTime:       time.Now().UnixMilli(),
				Enforcement:     EnforcementDefer,
			},
			shouldSetDeadline:    true,
			shouldSetEnforcement: false,
		},
		{
			name: "active deadline with enforce",
			ewc: ExpectWithinContext{
				RemainingMillis: 3000,
				StartTime:       time.Now().UnixMilli(),
				Enforcement:     EnforcementEnforce,
			},
			shouldSetDeadline:        true,
			shouldSetEnforcement:     true,
			expectedEnforcementValue: "true",
		},
		{
			name: "active deadline with disable",
			ewc: ExpectWithinContext{
				RemainingMillis: 2000,
				StartTime:       time.Now().UnixMilli(),
				Enforcement:     EnforcementDisable,
			},
			shouldSetDeadline:        true,
			shouldSetEnforcement:     true,
			expectedEnforcementValue: "false",
		},
		{
			name: "expired deadline",
			ewc: ExpectWithinContext{
				RemainingMillis: 100,
				StartTime:       time.Now().UnixMilli() - 200,
				Enforcement:     EnforcementEnforce,
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

	ewc, ok := GetExpectWithinFromContext(newCtx)
	require.True(t, ok)
	assert.Equal(t, int64(5000), ewc.RemainingMillis)
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
				ewc := ExpectWithinContext{
					RemainingMillis: 100,
					StartTime:       time.Now().UnixMilli() - 200,
					Enforcement:     EnforcementDefer,
				}
				return ContextWithExpectWithin(ctx, ewc)
			},
			expectRemaining: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setupContext()
			remaining := GetRemainingDeadline(ctx)

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
				ewc := ExpectWithinContext{
					RemainingMillis: 100,
					StartTime:       time.Now().UnixMilli() - 200,
					Enforcement:     EnforcementDefer,
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
				ewc := ExpectWithinContext{
					RemainingMillis: 100,
					StartTime:       time.Now().UnixMilli() - 200,
					Enforcement:     EnforcementEnforce,
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

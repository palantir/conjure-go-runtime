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
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeadlineExpiredReasonString(t *testing.T) {
	tests := []struct {
		name     string
		reason   DeadlineExpiredReason
		expected string
	}{
		{
			name:     "external",
			reason:   ReasonExternal,
			expected: "external",
		},
		{
			name:     "internal",
			reason:   ReasonInternal,
			expected: "internal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.reason.String())
		})
	}
}

func TestDeadlineExpiredReasonStatusCode(t *testing.T) {
	tests := []struct {
		name     string
		reason   DeadlineExpiredReason
		expected int
	}{
		{
			name:     "external",
			reason:   ReasonExternal,
			expected: 400,
		},
		{
			name:     "internal",
			reason:   ReasonInternal,
			expected: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.reason.StatusCode())
		})
	}
}

func TestDeadlineExpiredErrors(t *testing.T) {
	t.Run("external error message", func(t *testing.T) {
		assert.Equal(t, "An externally provided deadline for completing work has expired.", ErrDeadlineExpiredExternal.Error())
	})

	t.Run("internal error message", func(t *testing.T) {
		assert.Equal(t, "An internal deadline for completing work has expired.", ErrDeadlineExpiredInternal.Error())
	})

	t.Run("external error reason", func(t *testing.T) {
		assert.Equal(t, ReasonExternal, ErrDeadlineExpiredExternal.Reason())
	})

	t.Run("internal error reason", func(t *testing.T) {
		assert.Equal(t, ReasonInternal, ErrDeadlineExpiredInternal.Reason())
	})
}

func TestIsDeadlineExpiredExternal(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "external error",
			err:      ErrDeadlineExpiredExternal,
			expected: true,
		},
		{
			name:     "wrapped external error",
			err:      fmt.Errorf("wrapped: %w", ErrDeadlineExpiredExternal),
			expected: true,
		},
		{
			name:     "internal error",
			err:      ErrDeadlineExpiredInternal,
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDeadlineExpiredExternal(tt.err))
		})
	}
}

func TestIsDeadlineExpiredInternal(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "internal error",
			err:      ErrDeadlineExpiredInternal,
			expected: true,
		},
		{
			name:     "wrapped internal error",
			err:      fmt.Errorf("wrapped: %w", ErrDeadlineExpiredInternal),
			expected: true,
		},
		{
			name:     "external error",
			err:      ErrDeadlineExpiredExternal,
			expected: false,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDeadlineExpiredInternal(tt.err))
		})
	}
}

func TestIsDeadlineExpiredError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "external error",
			err:      ErrDeadlineExpiredExternal,
			expected: true,
		},
		{
			name:     "internal error",
			err:      ErrDeadlineExpiredInternal,
			expected: true,
		},
		{
			name:     "wrapped external error",
			err:      fmt.Errorf("wrapped: %w", ErrDeadlineExpiredExternal),
			expected: true,
		},
		{
			name:     "wrapped internal error",
			err:      fmt.Errorf("wrapped: %w", ErrDeadlineExpiredInternal),
			expected: true,
		},
		{
			name:     "other error",
			err:      errors.New("some error"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsDeadlineExpiredError(tt.err))
		})
	}
}

func TestGetDeadlineExpiredReason(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectNil      bool
		expectedReason DeadlineExpiredReason
	}{
		{
			name:           "external error",
			err:            ErrDeadlineExpiredExternal,
			expectNil:      false,
			expectedReason: ReasonExternal,
		},
		{
			name:           "internal error",
			err:            ErrDeadlineExpiredInternal,
			expectNil:      false,
			expectedReason: ReasonInternal,
		},
		{
			name:           "wrapped external error",
			err:            fmt.Errorf("wrapped: %w", ErrDeadlineExpiredExternal),
			expectNil:      false,
			expectedReason: ReasonExternal,
		},
		{
			name:           "wrapped internal error",
			err:            fmt.Errorf("wrapped: %w", ErrDeadlineExpiredInternal),
			expectNil:      false,
			expectedReason: ReasonInternal,
		},
		{
			name:      "other error",
			err:       errors.New("some error"),
			expectNil: true,
		},
		{
			name:      "nil error",
			err:       nil,
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason := GetDeadlineExpiredReason(tt.err)
			if tt.expectNil {
				assert.Nil(t, reason)
			} else {
				assert.NotNil(t, reason)
				assert.Equal(t, tt.expectedReason, *reason)
			}
		})
	}
}

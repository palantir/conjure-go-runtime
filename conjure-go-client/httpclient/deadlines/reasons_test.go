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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDeadlineExpiredToResponse(t *testing.T) {
	tests := []struct {
		name                 string
		err                  error
		expectEncoded        bool
		expectedStatus       int
		expectedReasonHeader string
	}{
		{
			name:                 "external error",
			err:                  ErrDeadlineExpiredExternal,
			expectEncoded:        true,
			expectedStatus:       400,
			expectedReasonHeader: "external",
		},
		{
			name:                 "internal error",
			err:                  ErrDeadlineExpiredInternal,
			expectEncoded:        true,
			expectedStatus:       500,
			expectedReasonHeader: "internal",
		},
		{
			name:                 "wrapped external error",
			err:                  fmt.Errorf("wrapped: %w", ErrDeadlineExpiredExternal),
			expectEncoded:        true,
			expectedStatus:       400,
			expectedReasonHeader: "external",
		},
		{
			name:          "other error",
			err:           errors.New("some error"),
			expectEncoded: false,
		},
		{
			name:          "nil error",
			err:           nil,
			expectEncoded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			encoded := EncodeDeadlineExpiredToResponse(tt.err, w)

			assert.Equal(t, tt.expectEncoded, encoded)

			if tt.expectEncoded {
				assert.Equal(t, tt.expectedStatus, w.Code)
				assert.Equal(t, tt.expectedReasonHeader, w.Header().Get(HeaderDeadlineExpiredReason))
			}
		})
	}
}

func TestParseDeadlineExpiredFromResponse(t *testing.T) {
	tests := []struct {
		name          string
		statusCode    int
		reasonHeader  string
		expectedError error
	}{
		{
			name:          "external error",
			statusCode:    400,
			reasonHeader:  "external",
			expectedError: ErrDeadlineExpiredExternal,
		},
		{
			name:          "internal error",
			statusCode:    500,
			reasonHeader:  "internal",
			expectedError: ErrDeadlineExpiredInternal,
		},
		{
			name:          "external case insensitive",
			statusCode:    400,
			reasonHeader:  "EXTERNAL",
			expectedError: ErrDeadlineExpiredExternal,
		},
		{
			name:          "internal case insensitive",
			statusCode:    500,
			reasonHeader:  "INTERNAL",
			expectedError: ErrDeadlineExpiredInternal,
		},
		{
			name:          "wrong status for external",
			statusCode:    500,
			reasonHeader:  "external",
			expectedError: nil,
		},
		{
			name:          "wrong status for internal",
			statusCode:    400,
			reasonHeader:  "internal",
			expectedError: nil,
		},
		{
			name:          "no header",
			statusCode:    400,
			reasonHeader:  "",
			expectedError: nil,
		},
		{
			name:          "unknown reason",
			statusCode:    400,
			reasonHeader:  "unknown",
			expectedError: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tt.statusCode,
				Header:     http.Header{},
			}
			if tt.reasonHeader != "" {
				resp.Header.Set(HeaderDeadlineExpiredReason, tt.reasonHeader)
			}

			err := ParseDeadlineExpiredFromResponse(resp)
			if tt.expectedError == nil {
				assert.Nil(t, err)
			} else {
				require.NotNil(t, err)
				assert.True(t, errors.Is(err, tt.expectedError))
			}
		})
	}
}

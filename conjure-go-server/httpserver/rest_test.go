// Copyright (c) 2022 Palantir Technologies. All rights reserved.
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

package httpserver

import (
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteJSONResponse(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		WriteJSONResponse(recorder, map[string]int{"a": 1}, http.StatusCreated)
		assert.Equal(t, http.StatusCreated, recorder.Code)
		assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "{\"a\":1}\n", recorder.Body.String())
	})
	t.Run("marshal error is a 500", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		WriteJSONResponse(recorder, math.NaN(), http.StatusOK)
		assert.Equal(t, http.StatusInternalServerError, recorder.Code)
		assert.Equal(t, "text/plain; charset=utf-8", recorder.Header().Get("Content-Type"))
	})
}

func TestValidateSecretStringEqual(t *testing.T) {
	for _, tc := range []struct {
		name        string
		a           string
		b           string
		expectMatch bool
	}{
		{
			name:        "empty strings match",
			a:           "",
			b:           "",
			expectMatch: true,
		},
		{
			name:        "strings match exactly",
			a:           "secret",
			b:           "secret",
			expectMatch: true,
		},
		{
			name:        "string does not match empty string",
			a:           "secret",
			b:           "",
			expectMatch: false,
		},
		{
			name:        "string does not match other string",
			a:           "secret",
			b:           "invalid",
			expectMatch: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectMatch, SecretStringEqual(tc.a, tc.b))
		})
	}
}

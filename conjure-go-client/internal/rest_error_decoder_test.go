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

package internal

import (
	"io"
	"net/http"
	"strings"
	"testing"

	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func errorResponse(status int, contentType, body string) *http.Response {
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// DecodeError caps the retained error body at maxErrorBodyBytes so a hostile server cannot
// amplify memory use, flagging the truncation via safe params.
func TestRESTErrorDecoder_CapsErrorBody(t *testing.T) {
	decoder := NewRESTErrorDecoder(nil)

	t.Run("over cap is truncated and flagged", func(t *testing.T) {
		body := strings.Repeat("A", maxErrorBodyBytes+512)
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", body))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.Equal(t, true, safe["responseBodyTruncated"])
		assert.Equal(t, maxErrorBodyBytes, safe["responseBodyLimitBytes"])
		retained, ok := unsafe["responseBody"].(string)
		require.True(t, ok, "responseBody should be present")
		assert.Len(t, retained, maxErrorBodyBytes, "retained body is capped at the limit")
	})

	t.Run("exactly at cap is not truncated", func(t *testing.T) {
		body := strings.Repeat("A", maxErrorBodyBytes)
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", body))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.NotContains(t, safe, "responseBodyTruncated")
		assert.NotContains(t, safe, "responseBodyLimitBytes")
		assert.Len(t, unsafe["responseBody"], maxErrorBodyBytes)
	})

	t.Run("small body is retained intact", func(t *testing.T) {
		err := decoder.DecodeError(errorResponse(http.StatusInternalServerError, "text/plain", "boom"))
		require.Error(t, err)
		safe, unsafe := werror.ParamsFromError(err)
		assert.NotContains(t, safe, "responseBodyTruncated")
		assert.Equal(t, "boom", unsafe["responseBody"])
	})
}

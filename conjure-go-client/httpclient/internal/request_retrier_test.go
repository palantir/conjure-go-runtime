// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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
	"context"
	"net/http"
	"testing"
	"time"

	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/require"
)

func TestRequestRetrier_HandleMeshURI(t *testing.T) {
	r := NewRequestRetrier([]string{"mesh-http://example.com"}, 1)
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "http://example.com")

	respErr := werror.ErrorWithContextParams(context.Background(), "error", werror.SafeParam("statusCode", 429))
	uri, _ = r.GetNextURI(nil, respErr)
	require.Empty(t, uri)
}

func TestRequestRetrier_AttemptCount(t *testing.T) {
	maxAttempts := 3
	r := NewRequestRetrier([]string{"https://example.com"}, maxAttempts)
	// first request is not a retry
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "https://example.com")

	for i := 0; i < maxAttempts-1; i++ {
		uri, _ = r.GetNextURI(nil, nil)
		require.Equal(t, uri, "https://example.com")
	}
	uri, _ = r.GetNextURI(nil, nil)
	require.Empty(t, uri)
}

func TestRequestRetrier_UnlimitedAttempts(t *testing.T) {
	_, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := NewRequestRetrier([]string{"https://example.com"}, 0)

	for i := 0; i <= 10; i++ {
		uri, _ := r.GetNextURI(nil, nil)
		require.Equal(t, uri, "https://example.com")
	}

	// Success should stop retries
	uri, _ := r.GetNextURI(&http.Response{StatusCode: 200}, nil)
	require.Empty(t, uri)
}

func TestRequestRetrier_UsesLocationHeader(t *testing.T) {
	respWithLocationHeader := &http.Response{
		StatusCode: StatusCodeRetryOther,
		Header:     http.Header{"Location": []string{"http://example.com"}},
	}

	r := NewRequestRetrier([]string{"a"}, 2)
	uri, isRelocated := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "a")
	require.False(t, isRelocated)

	uri, isRelocated = r.GetNextURI(respWithLocationHeader, nil)
	require.Equal(t, uri, "http://example.com")
	require.True(t, isRelocated)
}

func TestRequestRetrier_UsesLocationFromErr(t *testing.T) {
	r := NewRequestRetrier([]string{"http://example-1.com"}, 2)
	respErr := werror.ErrorWithContextParams(context.Background(), "307",
		werror.SafeParam("statusCode", 307),
		werror.SafeParam("location", "http://example-2.com"))

	uri, isRelocated := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "http://example-1.com")
	require.False(t, isRelocated)

	uri, isRelocated = r.GetNextURI(nil, respErr)
	require.Equal(t, uri, "http://example-2.com")
	require.True(t, isRelocated)
}

func TestRequestRetrier_GetNextURI(t *testing.T) {
	for _, tc := range []struct {
		name               string
		resp               *http.Response
		respErr            error
		uris               []string
		shouldRetry        bool
		shouldRetrySameURI bool
	}{
		{
			name:               "returns error if response exists and doesn't appear retryable",
			resp:               &http.Response{},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
		{
			name:               "returns error if error code not retryable",
			resp:               &http.Response{},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
		{
			name:               "returns a URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name:               "returns a URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name:               "retries and backs off the single URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
		},
		{
			name:               "returns a new URI if unavailable",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "503", werror.SafeParam("statusCode", 503)),
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name:               "retries and backs off the single URI if unavailable",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "503", werror.SafeParam("statusCode", 503)),
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
		},
		{
			name:               "returns a new URI and backs off if throttled",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "429", werror.SafeParam("statusCode", 429)),
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name:               "retries single URI and backs off if throttled",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "429", werror.SafeParam("statusCode", 429)),
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
		},
		{
			name: "retries another URI if gets retry other response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryOther,
			},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name: "retries single URI and backs off if gets retry other response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryOther,
			},
			respErr:            nil,
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
		},
		{
			name: "retries another URI if gets retry temporary redirect response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryTemporaryRedirect,
			},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
		},
		{
			name: "retries single URI and backs off if gets retry temporary redirect response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryTemporaryRedirect,
			},
			respErr:            nil,
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
		},
		{
			name: "does not retry 400 responses",
			resp: &http.Response{
				StatusCode: 400,
			},
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
		{
			name: "does not retry 404 responses",
			resp: &http.Response{
				StatusCode: 404,
			},
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
		{
			name:               "does not retry 400 errors",
			respErr:            werror.ErrorWithContextParams(context.Background(), "400", werror.SafeParam("statusCode", 400)),
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
		{
			name:               "does not retry 404s",
			respErr:            werror.ErrorWithContextParams(context.Background(), "404", werror.SafeParam("statusCode", 404)),
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRequestRetrier(tc.uris, 2)
			// first URI isn't a retry
			firstURI, _ := r.GetNextURI(nil, nil)
			require.NotEmpty(t, firstURI)

			retryURI, _ := r.GetNextURI(tc.resp, tc.respErr)
			if tc.shouldRetry {
				require.Contains(t, tc.uris, retryURI)
				if tc.shouldRetrySameURI {
					require.Equal(t, retryURI, firstURI)
				} else {
					require.NotEqual(t, retryURI, firstURI)
				}
			} else {
				require.Empty(t, retryURI)
			}
		})
	}
}

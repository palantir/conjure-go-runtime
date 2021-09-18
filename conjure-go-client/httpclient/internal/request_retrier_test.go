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

	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ retry.Retrier = &mockRetrier{}

func TestRequestRetrier_HandleMeshURI(t *testing.T) {
	r := NewRequestRetrier([]string{"mesh-http://example.com"}, retry.Start(context.Background()), 1)
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "http://example.com")

	respErr := werror.ErrorWithContextParams(context.Background(), "error", werror.SafeParam("statusCode", 429))
	uri, _ = r.GetNextURI(nil, respErr)
	require.Empty(t, uri)
}

func TestRequestRetrier_AttemptCount(t *testing.T) {
	maxAttempts := 3
	r := NewRequestRetrier([]string{"https://example.com"}, retry.Start(context.Background()), maxAttempts)
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	r := NewRequestRetrier([]string{"https://example.com"}, retry.Start(ctx, retry.WithInitialBackoff(50*time.Millisecond), retry.WithRandomizationFactor(0)), 0)

	startTime := time.Now()
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "https://example.com")
	require.Lessf(t, time.Since(startTime), 49*time.Millisecond, "first GetNextURI should not have any delay")

	startTime = time.Now()
	uri, _ = r.GetNextURI(nil, nil)
	require.Equal(t, uri, "https://example.com")
	assert.Greater(t, time.Since(startTime), 50*time.Millisecond, "delay should be at least 1 backoff")
	assert.Less(t, time.Since(startTime), 100*time.Millisecond, "delay should be less than 2 backoffs")

	startTime = time.Now()
	uri, _ = r.GetNextURI(nil, nil)
	require.Equal(t, uri, "https://example.com")
	assert.Greater(t, time.Since(startTime), 100*time.Millisecond, "delay should be at least 2 backoffs")
	assert.Less(t, time.Since(startTime), 200*time.Millisecond, "delay should be less than 3 backoffs")

	// Success should stop retries
	uri, _ = r.GetNextURI(&http.Response{StatusCode: 200}, nil)
	require.Empty(t, uri)
}

func TestRequestRetrier_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := NewRequestRetrier([]string{"https://example.com"}, retry.Start(ctx), 0)

	// First attempt should return a URI to ensure that the client can instrument the request even
	// if the context is done
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "https://example.com")

	// Subsequent attempt should stop retries
	uri, _ = r.GetNextURI(nil, nil)
	require.Empty(t, uri)
}

func TestRequestRetrier_UsesLocationHeader(t *testing.T) {
	respWithLocationHeader := &http.Response{
		StatusCode: StatusCodeRetryOther,
		Header:     http.Header{"Location": []string{"http://example.com"}},
	}

	r := NewRequestRetrier([]string{"a"}, retry.Start(context.Background()), 2)
	uri, isRelocated := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "a")
	require.False(t, isRelocated)

	uri, isRelocated = r.GetNextURI(respWithLocationHeader, nil)
	require.Equal(t, uri, "http://example.com")
	require.True(t, isRelocated)
}

func TestRequestRetrier_UsesLocationFromErr(t *testing.T) {
	r := NewRequestRetrier([]string{"http://example-1.com"}, retry.Start(context.Background()), 2)
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
		shouldRetryBackoff bool
		shouldRetryReset   bool
	}{
		{
			name:               "returns error if response exists and doesn't appear retryable",
			resp:               &http.Response{},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "returns error if error code not retryable",
			resp:               &http.Response{},
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        false,
			shouldRetrySameURI: false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "returns a URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "returns a URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "retries and backs off the single URI if response and error are nil",
			resp:               nil,
			respErr:            nil,
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name:               "returns a new URI if unavailable",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "503", werror.SafeParam("statusCode", 503)),
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "retries and backs off the single URI if unavailable",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "503", werror.SafeParam("statusCode", 503)),
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name:               "returns a new URI and backs off if throttled",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "429", werror.SafeParam("statusCode", 429)),
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetrySameURI: false,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name:               "retries single URI and backs off if throttled",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "429", werror.SafeParam("statusCode", 429)),
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
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
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
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
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
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
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
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
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retrier := newMockRetrier()
			r := NewRequestRetrier(tc.uris, retrier, 2)
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
				if tc.shouldRetryReset {
					require.True(t, retrier.DidReset)
				}
				if tc.shouldRetryBackoff {
					require.True(t, retrier.DidGetNext)
				}
			} else {
				require.Empty(t, retryURI)
			}
		})
	}
}

func newMockRetrier() *mockRetrier {
	return &mockRetrier{
		DidGetNext: false,
		DidReset:   false,
	}
}

type mockRetrier struct {
	DidGetNext bool
	DidReset   bool
}

func (m *mockRetrier) Reset() {
	m.DidReset = true
}

func (m *mockRetrier) Next() bool {
	m.DidGetNext = true
	return true
}

func (m *mockRetrier) CurrentAttempt() int {
	return 0
}

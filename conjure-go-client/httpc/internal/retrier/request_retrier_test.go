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

package retrier

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/palantir/pkg/retry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ retry.Retrier = &mockRetrier{}

func TestRequestRetrier_HandleMeshURI(t *testing.T) {
	r := NewRequestRetrier([]string{"mesh-http://example.com"}, retry.Start(context.Background()), 1)
	uri, _ := r.GetNextURI(nil, nil)
	require.Equal(t, uri, "http://example.com")

	// A retryable (429) response still does not retry a mesh URI.
	uri, _ = r.GetNextURI(&http.Response{StatusCode: http.StatusTooManyRequests}, nil)
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

func TestRequestRetrier_GetNextURI(t *testing.T) {
	for _, tc := range []struct {
		name               string
		resp               *http.Response
		uris               []string
		shouldRetry        bool
		shouldRetrySameURI bool
		shouldRetryBackoff bool
		shouldRetryReset   bool
	}{
		{
			name:        "returns error if response exists and doesn't appear retryable",
			resp:        &http.Response{},
			uris:        []string{"a", "b"},
			shouldRetry: false,
		},
		{
			name:        "returns a URI if response is nil",
			resp:        nil,
			uris:        []string{"a", "b"},
			shouldRetry: true,
		},
		{
			name:               "retries and backs off the single URI if response is nil",
			resp:               nil,
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
		},
		{
			name:        "returns a new URI if unavailable",
			resp:        &http.Response{StatusCode: http.StatusServiceUnavailable},
			uris:        []string{"a", "b"},
			shouldRetry: true,
		},
		{
			name:               "retries and backs off the single URI if unavailable",
			resp:               &http.Response{StatusCode: http.StatusServiceUnavailable},
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
		},
		{
			name:               "returns a new URI and backs off if throttled",
			resp:               &http.Response{StatusCode: http.StatusTooManyRequests},
			uris:               []string{"a", "b"},
			shouldRetry:        true,
			shouldRetryBackoff: true,
		},
		{
			name:               "retries single URI and backs off if throttled",
			resp:               &http.Response{StatusCode: http.StatusTooManyRequests},
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
		},
		{
			name:        "retries another URI if gets retry other response without location",
			resp:        &http.Response{StatusCode: StatusCodeRetryOther},
			uris:        []string{"a", "b"},
			shouldRetry: true,
		},
		{
			name:               "retries single URI and backs off if gets retry other response without location",
			resp:               &http.Response{StatusCode: StatusCodeRetryOther},
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
		},
		{
			name:        "retries another URI if gets retry temporary redirect response without location",
			resp:        &http.Response{StatusCode: StatusCodeRetryTemporaryRedirect},
			uris:        []string{"a", "b"},
			shouldRetry: true,
		},
		{
			name:               "retries single URI and backs off if gets retry temporary redirect response without location",
			resp:               &http.Response{StatusCode: StatusCodeRetryTemporaryRedirect},
			uris:               []string{"a"},
			shouldRetry:        true,
			shouldRetrySameURI: true,
			shouldRetryBackoff: true,
		},
		{
			name:        "does not retry 400 responses",
			resp:        &http.Response{StatusCode: 400},
			uris:        []string{"a", "b"},
			shouldRetry: false,
		},
		{
			name:        "does not retry 404 responses",
			resp:        &http.Response{StatusCode: 404},
			uris:        []string{"a", "b"},
			shouldRetry: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retrier := newMockRetrier()
			r := NewRequestRetrier(tc.uris, retrier, 2)
			// first URI isn't a retry
			firstURI, _ := r.GetNextURI(nil, nil)
			require.NotEmpty(t, firstURI)

			retryURI, _ := r.GetNextURI(tc.resp, nil)
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

func TestRetryResponseParsers(t *testing.T) {
	for _, test := range []struct {
		Name             string
		Response         *http.Response
		IsRetryOther     bool
		RetryOtherURL    string
		IsThrottle       bool
		ThrottleDuration time.Duration
		IsUnavailable    bool
	}{
		{
			Name:     "200 OK",
			Response: &http.Response{Header: http.Header{}, StatusCode: 200},
		},
		{
			Name:         "307 RetryTemporaryRedirect without Location",
			Response:     &http.Response{Header: http.Header{}, StatusCode: 307},
			IsRetryOther: true,
		},
		{
			Name:          "307 RetryTemporaryRedirect with Location",
			Response:      &http.Response{Header: http.Header{"Location": []string{"https://host-2:8443/app"}}, StatusCode: 307},
			IsRetryOther:  true,
			RetryOtherURL: "https://host-2:8443/app",
		},
		{
			Name:         "308 RetryOther without Location",
			Response:     &http.Response{Header: http.Header{}, StatusCode: 308},
			IsRetryOther: true,
		},
		{
			Name:          "308 RetryOther with Location",
			Response:      &http.Response{Header: http.Header{"Location": []string{"https://host-2:8443/app"}}, StatusCode: 308},
			IsRetryOther:  true,
			RetryOtherURL: "https://host-2:8443/app",
		},
		{
			Name:       "429 throttle without Retry-After",
			Response:   &http.Response{Header: http.Header{}, StatusCode: 429},
			IsThrottle: true,
		},
		{
			Name:          "503 unavailable",
			Response:      &http.Response{Header: http.Header{}, StatusCode: 503},
			IsUnavailable: true,
		},
		{
			Name:             "429 throttle with Retry-After seconds",
			Response:         &http.Response{Header: http.Header{"Retry-After": []string{"60"}}, StatusCode: 429},
			IsThrottle:       true,
			ThrottleDuration: time.Minute,
		},
		{
			Name:             "429 throttle with Retry-After Date",
			Response:         &http.Response{Header: http.Header{"Retry-After": []string{time.Now().UTC().Add(time.Minute).Format(http.TimeFormat)}}, StatusCode: 429},
			IsThrottle:       true,
			ThrottleDuration: time.Minute,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			isRetryOther, retryOtherURL := isRetryOtherResponse(test.Response)
			if assert.Equal(t, test.IsRetryOther, isRetryOther) && test.RetryOtherURL != "" {
				if assert.NotNil(t, retryOtherURL) {
					assert.Equal(t, test.RetryOtherURL, retryOtherURL.String())
				}
			}

			isThrottle, throttleDur := isThrottleResponse(test.Response)
			if assert.Equal(t, test.IsThrottle, isThrottle) {
				assert.WithinDuration(t, time.Now().Add(test.ThrottleDuration), time.Now().Add(throttleDur), time.Second)
			}

			isUnavailable := isUnavailableResponse(test.Response)
			assert.Equal(t, test.IsUnavailable, isUnavailable)
		})
	}
}

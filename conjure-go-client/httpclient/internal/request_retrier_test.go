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
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestRetrier_HandleMeshURI(t *testing.T) {
	r := NewRequestRetrier(retry.Start(context.Background()), 1)
	req, err := http.NewRequest("GET", "mesh-http://example.com", nil)
	require.NoError(t, err)
	shouldRetry, _ := r.Next(&http.Response{Request: req}, nil)
	require.False(t, shouldRetry)
}

func TestRequestRetrier_AttemptCount(t *testing.T) {
	maxAttempts := 3
	err := errors.New("error")

	r := NewRequestRetrier(retry.Start(context.Background()), maxAttempts)
	// first request is not a retry, so it doesn't increment the overall count
	shouldRetry, _ := r.Next(nil, err)
	require.True(t, shouldRetry)

	for i := 0; i < maxAttempts-1; i++ {
		shouldRetry, _ = r.Next(nil, err)
		require.True(t, shouldRetry)
	}

	shouldRetry, _ = r.Next(nil, nil)
	require.False(t, shouldRetry)
}

func TestRequestRetrier_UnlimitedAttempts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	err := errors.New("error")
	r := NewRequestRetrier(retry.Start(ctx, retry.WithInitialBackoff(50*time.Millisecond), retry.WithRandomizationFactor(0)), 0)

	startTime := time.Now()
	shouldRetry, _ := r.Next(nil, nil)
	require.True(t, shouldRetry)
	require.Lessf(t, time.Since(startTime), 49*time.Millisecond, "first GetNextURI should not have any delay")

	startTime = time.Now()
	shouldRetry, _ = r.Next(nil, err)
	require.True(t, shouldRetry)
	assert.Greater(t, time.Since(startTime), 50*time.Millisecond, "delay should be at least 1 backoff")
	assert.Less(t, time.Since(startTime), 100*time.Millisecond, "delay should be less than 2 backoffs")

	startTime = time.Now()
	shouldRetry, _ = r.Next(nil, err)
	require.True(t, shouldRetry)
	assert.Greater(t, time.Since(startTime), 100*time.Millisecond, "delay should be at least 2 backoffs")
	assert.Less(t, time.Since(startTime), 200*time.Millisecond, "delay should be less than 3 backoffs")

	// Success should stop retries
	shouldRetry, _ = r.Next(&http.Response{StatusCode: http.StatusOK}, nil)
	require.False(t, shouldRetry)
}

func TestRequestRetrier_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := NewRequestRetrier(retry.Start(ctx), 0)

	// No retries if context is cancelled
	shouldRetry, _ := r.Next(nil, nil)
	require.False(t, shouldRetry)
}

func TestRequestRetrier_UsesLocationHeader(t *testing.T) {
	respWithLocationHeader := &http.Response{
		StatusCode: StatusCodeRetryOther,
		Header:     http.Header{"Location": []string{"http://example.com"}},
	}

	r := NewRequestRetrier(retry.Start(context.Background()), 2)
	shouldRetry, uri := r.Next(nil, nil)
	require.True(t, shouldRetry)
	require.Nil(t, uri)

	shouldRetry, uri = r.Next(respWithLocationHeader, nil)
	require.Equal(t, uri.String(), "http://example.com")
	require.True(t, shouldRetry)
}

func TestRequestRetrier_UsesLocationFromErr(t *testing.T) {
	r := NewRequestRetrier(retry.Start(context.Background()), 2)
	respErr := werror.ErrorWithContextParams(context.Background(), "307",
		werror.SafeParam("statusCode", 307),
		werror.SafeParam("location", "http://example-2.com"))

	shouldRetry, uri := r.Next(nil, respErr)
	require.NotNil(t, uri)
	require.Equal(t, uri.String(), "http://example-2.com")
	require.True(t, shouldRetry)
}

func TestRequestRetrier_Next(t *testing.T) {
	for _, tc := range []struct {
		name               string
		resp               *http.Response
		respErr            error
		retryURI           *url.URL
		shouldRetry        bool
		shouldRetryBackoff bool
		shouldRetryReset   bool
	}{
		{
			name:               "retries if unavailable",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "503", werror.SafeParam("statusCode", 503)),
			shouldRetry:        true,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "retries and backs off if throttled",
			resp:               nil,
			respErr:            werror.ErrorWithContextParams(context.Background(), "429", werror.SafeParam("statusCode", 429)),
			shouldRetry:        true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name: "retries with no backoff if gets retry other response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryOther,
			},
			respErr:            nil,
			shouldRetry:        true,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name: "retries with no backoff if gets retry temporary redirect response without location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryTemporaryRedirect,
			},
			respErr:            nil,
			shouldRetry:        true,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name: "retries with no backoff if gets retry temporary redirect response with a location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryTemporaryRedirect,
			},
			respErr: werror.ErrorWithContextParams(context.Background(),
				"307",
				werror.SafeParam("statusCode", 307),
				werror.SafeParam("location", "http://example-2.com")),
			retryURI:           mustNewURL("http://example-2.com"),
			shouldRetry:        true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name: "retries with no backoff if gets retry other redirect response with a location",
			resp: &http.Response{
				StatusCode: StatusCodeRetryOther,
			},
			respErr: werror.ErrorWithContextParams(context.Background(),
				"308",
				werror.SafeParam("statusCode", 308),
				werror.SafeParam("location", "http://example-2.com")),
			retryURI:           mustNewURL("http://example-2.com"),
			shouldRetry:        true,
			shouldRetryBackoff: true,
			shouldRetryReset:   false,
		},
		{
			name: "does not retry 400 responses",
			resp: &http.Response{
				StatusCode: 400,
			},
			shouldRetry:        false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name: "does not retry 404 responses",
			resp: &http.Response{
				StatusCode: 404,
			},
			shouldRetry:        false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "does not retry 400 errors",
			respErr:            werror.ErrorWithContextParams(context.Background(), "400", werror.SafeParam("statusCode", 400)),
			shouldRetry:        false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
		{
			name:               "does not retry 404s",
			respErr:            werror.ErrorWithContextParams(context.Background(), "404", werror.SafeParam("statusCode", 404)),
			shouldRetry:        false,
			shouldRetryBackoff: false,
			shouldRetryReset:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			retrier := newMockRetrier()
			r := NewRequestRetrier(retrier, 2)
			// first URI isn't a retry
			shouldRetry, _ := r.Next(nil, nil)
			require.True(t, shouldRetry)

			shouldRetry, retryURI := r.Next(tc.resp, tc.respErr)
			assert.Equal(t, tc.shouldRetry, shouldRetry)
			if tc.shouldRetry {
				if tc.retryURI != nil {
					require.Equal(t, tc.retryURI.String(), retryURI.String())
				}
				if tc.shouldRetryReset {
					require.True(t, retrier.DidReset)
				}
				if tc.shouldRetryBackoff {
					require.True(t, retrier.DidGetNext)
				}
			} else {
				require.Nil(t, retryURI)
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

func mustNewURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}

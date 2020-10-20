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

	"github.com/palantir/pkg/retry"
	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/require"
)

var _ retry.Retrier = &mockRetrier{}

func TestRequestRetrier_HandleMeshURI(t *testing.T) {
	ctx := context.Background()
	r := NewRequestRetrier([]string{"mesh-http://example.com"}, retry.Start(context.Background()), 1)
	require.True(t, r.ShouldGetNextURI(nil, nil))
	uri, err := r.GetNextURI(ctx, nil, nil)
	require.NoError(t, err)
	require.Equal(t, uri, "http://example.com")
	respErr := werror.ErrorWithContextParams(context.Background(), "error", werror.SafeParam("statusCode", 429))
	require.False(t, r.ShouldGetNextURI(nil, respErr))
	_, err = r.GetNextURI(ctx, nil, respErr)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetNextURI called, but retry should not be attempted")
}

func TestRequestRetrier_RetryCount(t *testing.T) {
	ctx := context.Background()
	maxRetries := 3
	r := NewRequestRetrier([]string{"https://example.com"}, retry.Start(context.Background()), maxRetries)
	require.True(t, r.ShouldGetNextURI(nil, nil))
	// first request is not a retry
	_, err := r.GetNextURI(ctx, nil, nil)
	require.NoError(t, err)
	for i := 0; i < maxRetries; i++ {
		_, err = r.GetNextURI(ctx, nil, nil)
		require.NoError(t, err)
	}
	require.False(t, r.ShouldGetNextURI(nil, nil))
	_, err = r.GetNextURI(ctx, nil, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "GetNextURI called, but retry should not be attempted")
}

func TestRequestRetrier_UsesLocationHeader(t *testing.T) {
	respWithLocationHeader := &http.Response{
		StatusCode: StatusCodeRetryOther,
		Header:     map[string][]string{},
	}
	respWithLocationHeader.Header.Add("Location", "http://example.com")
	ctx := context.Background()
	r := NewRequestRetrier([]string{"a"}, retry.Start(context.Background()), 1)
	require.True(t, r.ShouldGetNextURI(nil, nil))
	_, err := r.GetNextURI(ctx, nil, nil)
	require.NoError(t, err)
	require.True(t, r.ShouldGetNextURI(respWithLocationHeader, nil))
	uri, err := r.GetNextURI(ctx, respWithLocationHeader, nil)
	require.NoError(t, err)
	require.Equal(t, uri, "http://example.com")
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			retrier := newMockRetrier()
			r := NewRequestRetrier(tc.uris, retrier, 1)
			// first URI isn't a retry
			firstURI, _ := r.GetNextURI(ctx, nil, nil)
			if !tc.shouldRetry {
				require.False(t, r.ShouldGetNextURI(tc.resp, tc.respErr))
			} else {
				require.True(t, r.ShouldGetNextURI(tc.resp, tc.respErr))
			}
			retryURI, err := r.GetNextURI(ctx, tc.resp, tc.respErr)
			if tc.shouldRetry {
				require.NoError(t, err)
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
				require.Error(t, err)
				require.Contains(t, err.Error(), "GetNextURI called, but retry should not be attempted")
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

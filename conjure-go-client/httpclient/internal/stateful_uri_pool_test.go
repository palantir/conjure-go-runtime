package internal

import (
	"net/http"
	"testing"

	"github.com/palantir/pkg/refreshable"
	"github.com/stretchr/testify/assert"
)

//func TestRequestRetrier_UsesLocationFromErr(t *testing.T) {
//	r := NewRequestRetrier([]string{"http://example-1.com"}, retry.Start(context.Background()), 2)
//	respErr := werror.ErrorWithContextParams(context.Background(), "307",
//		werror.SafeParam("statusCode", 307),
//		werror.SafeParam("location", "http://example-2.com"))
//
//	uri, isRelocated := r.GetNextURI(nil, nil)
//	require.Equal(t, uri, "http://example-1.com")
//	require.False(t, isRelocated)
//
//	uri, isRelocated = r.GetNextURI(nil, respErr)
//	require.Equal(t, uri, "http://example-2.com")
//	require.True(t, isRelocated)
//}

func TestRequestRetrier_GetNextURIs(t *testing.T) {
	for _, tc := range []struct {
		name               string
		resp               *http.Response
		chosenURI          string
		beforeURIs         []string
		afterURIs          []string
		shouldRetry        bool
		shouldRetrySameURI bool
		shouldRetryBackoff bool
		shouldRetryReset   bool
	}{
		{
			name:       "preserves chosen URI if response doesn't contain a handled status code",
			resp:       &http.Response{},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain0.example.com", "https://domain1.example.com"},
		},
		{
			name:       "remove chosen URI if response is nil",
			resp:       nil,
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain1.example.com"},
		},
		{
			name:       "removes chosen URI if response return code is 503",
			resp:       &http.Response{StatusCode: http.StatusServiceUnavailable},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain1.example.com"},
		},
		{
			name:       "preserves chosen URI if response return code is 503 but it's a single URI",
			resp:       &http.Response{StatusCode: http.StatusServiceUnavailable},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com"},
			afterURIs:  []string{"https://domain0.example.com"},
		},
		{
			name:       "removes chosen URI if response return code is 429",
			resp:       &http.Response{StatusCode: http.StatusTooManyRequests},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain1.example.com"},
		},
		{
			name:       "preserves chosen URI if response return code is 429 but it's a single URI",
			resp:       &http.Response{StatusCode: http.StatusTooManyRequests},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com"},
			afterURIs:  []string{"https://domain0.example.com"},
		},
		//{
		//	name: "retries another URI if gets retry other response without location",
		//	resp: &http.Response{StatusCode: StatusCodeRetryOther},
		//	resp: &http.Response{
		//		StatusCode: StatusCodeRetryOther,
		//	},
		//	respErr:            nil,
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        true,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name: "retries single URI and backs off if gets retry other response without location",
		//	resp: &http.Response{
		//		StatusCode: StatusCodeRetryOther,
		//	},
		//	respErr:            nil,
		//	uris:               []string{"https://domain0.example.com"},
		//	shouldRetry:        true,
		//	shouldRetrySameURI: true,
		//	shouldRetryBackoff: true,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name: "retries another URI if gets retry temporary redirect response without location",
		//	resp: &http.Response{
		//		StatusCode: StatusCodeRetryTemporaryRedirect,
		//	},
		//	respErr:            nil,
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        true,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name: "retries single URI and backs off if gets retry temporary redirect response without location",
		//	resp: &http.Response{
		//		StatusCode: StatusCodeRetryTemporaryRedirect,
		//	},
		//	respErr:            nil,
		//	uris:               []string{"https://domain0.example.com"},
		//	shouldRetry:        true,
		//	shouldRetrySameURI: true,
		//	shouldRetryBackoff: true,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name: "does not retry 400 responses",
		//	resp: &http.Response{
		//		StatusCode: 400,
		//	},
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        false,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name: "does not retry 404 responses",
		//	resp: &http.Response{
		//		StatusCode: 404,
		//	},
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        false,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name:               "does not retry 400 errors",
		//	respErr:            werror.ErrorWithContextParams(context.Background(), "400", werror.SafeParam("statusCode", 400)),
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        false,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
		//{
		//	name:               "does not retry 404s",
		//	respErr:            werror.ErrorWithContextParams(context.Background(), "404", werror.SafeParam("statusCode", 404)),
		//	uris:               []string{"https://domain0.example.com", "https://domain1.example.com"},
		//	shouldRetry:        false,
		//	shouldRetrySameURI: false,
		//	shouldRetryBackoff: false,
		//	shouldRetryReset:   false,
		//},
	} {
		t.Run(tc.name, func(t *testing.T) {
			//retrier := newMockRetrier()
			//r := NewRequestRetrier(tc.uris, retrier, 2)
			//// first URI isn't a retry
			//firstURI, _ := r.GetNextURI(nil, nil)
			//require.NotEmpty(t, firstURI)

			ref := refreshable.NewDefaultRefreshable(tc.beforeURIs)
			pool := NewStatefulURIPool(refreshable.NewStringSlice(ref))

			req, err := http.NewRequest("GET", tc.chosenURI, nil)
			assert.NoError(t, err)

			resp, err := pool.RoundTrip(req, newMockRoundTripper(tc.resp))
			assert.Equal(t, tc.resp, resp)
			assert.NoError(t, err)

			assert.ElementsMatch(t, tc.afterURIs, pool.URIs())

			//retryURI, _ := r.GetNextURI(tc.resp, tc.respErr)
			//if tc.shouldRetry {
			//	require.Contains(t, tc.uris, retryURI)
			//	if tc.shouldRetrySameURI {
			//		require.Equal(t, retryURI, firstURI)
			//	} else {
			//		require.NotEqual(t, retryURI, firstURI)
			//	}
			//	if tc.shouldRetryReset {
			//		require.True(t, retrier.DidReset)
			//	}
			//	if tc.shouldRetryBackoff {
			//		require.True(t, retrier.DidGetNext)
			//	}
			//} else {
			//	require.Empty(t, retryURI)
			//}
		})
	}
}

func newMockRoundTripper(resp *http.Response) http.RoundTripper {
	return &mockRoundTripper{resp: resp}
}

type mockRoundTripper struct {
	resp *http.Response
}

func (m mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.resp, nil
}

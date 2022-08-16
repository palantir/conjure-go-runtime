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

package internal

import (
	"net/http"
	"testing"

	"github.com/palantir/pkg/refreshable"
	"github.com/stretchr/testify/assert"
)

func TestRequestRetrier_GetNextURIs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		resp       *http.Response
		chosenURI  string
		beforeURIs []string
		afterURIs  []string
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
		{
			name:       "preserves chosen URI on permanent redirects",
			resp:       &http.Response{StatusCode: StatusCodeRetryOther},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain0.example.com", "https://domain1.example.com"},
		},
		{
			name:       "preserves chosen URI on temporary redirects",
			resp:       &http.Response{StatusCode: StatusCodeRetryOther},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain0.example.com", "https://domain1.example.com"},
		},
		{
			name:       "preserves chosen URI on 400 responses",
			resp:       &http.Response{StatusCode: http.StatusBadRequest},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain0.example.com", "https://domain1.example.com"},
		},
		{
			name:       "preserves chosen URI on 404 responses",
			resp:       &http.Response{StatusCode: http.StatusNotFound},
			chosenURI:  "https://domain0.example.com",
			beforeURIs: []string{"https://domain0.example.com", "https://domain1.example.com"},
			afterURIs:  []string{"https://domain0.example.com", "https://domain1.example.com"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ref := refreshable.NewDefaultRefreshable(tc.beforeURIs)
			pool := NewStatefulURIPool(refreshable.NewStringSlice(ref))

			req, err := http.NewRequest("GET", tc.chosenURI, nil)
			assert.NoError(t, err)

			resp, err := pool.RoundTrip(req, newMockRoundTripper(tc.resp))
			assert.Equal(t, tc.resp, resp)
			assert.NoError(t, err)

			assert.ElementsMatch(t, tc.afterURIs, pool.URIs())
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

// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiddlewareOrdering(t *testing.T) {
	for _, tc := range []struct {
		Name          string
		Config        ClientConfig
		ClientParams  []ClientParam
		RequestParams []RequestParam
		ExpectHeaders http.Header
	}{
		{
			Name:          "no middleware",
			ExpectHeaders: http.Header{},
		},
		{
			Name:         "WithSetHeader middleware",
			ClientParams: []ClientParam{WithSetHeader("X-Test", "value1"), WithSetHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			Name:         "WithAddHeader middleware",
			ClientParams: []ClientParam{WithAddHeader("X-Test", "value1"), WithAddHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
		{
			Name:         "WithAddHeader adds to WithSetHeader middleware",
			ClientParams: []ClientParam{WithSetHeader("X-Test", "value1"), WithAddHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
		{
			Name:         "WithSetHeader overwrites WithAddHeader middleware",
			ClientParams: []ClientParam{WithAddHeader("X-Test", "value1"), WithSetHeader("X-Test", "value2")},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2"},
			},
		},
		{
			// Builder Set/AddHeader are resolved by the decoration that runs after
			// the inner middleware, so the AddHeader contributor appends to the
			// value the inner middleware set.
			Name: "WithAddHeader appends after WithInnerMiddleware Set",
			ClientParams: []ClientParam{
				WithAddHeader("X-Test", "value1"),
				WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Set("X-Test", "value2")
					return next.RoundTrip(req)
				})),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2", "value1"},
			},
		},
		{
			Name: "WithAddHeader middleware adds to WithInnerMiddleware",
			ClientParams: []ClientParam{
				WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Set("X-Test", "value1")
					return next.RoundTrip(req)
				})),
				WithAddHeader("X-Test", "value2"),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value1", "value2"},
			},
		},
		{
			Name: "WithAddHeader appends after WithInnerMiddleware Add",
			ClientParams: []ClientParam{
				WithAddHeader("X-Test", "value1"),
				WithInnerMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Add("X-Test", "value2")
					return next.RoundTrip(req)
				})),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2", "value1"},
			},
		},
		{
			// This test demonstrates the 'wrapping' of WithMiddleware. The outermost middleware
			// sees the request first, so request modifications are applied in a 'last-applied-first-executed' order.
			// Note the expected "value2", "value1" output order.
			Name: "WithMiddleware adds to WithAddHeader",
			ClientParams: []ClientParam{
				WithAddHeader("X-Test", "value1"),
				WithMiddleware(MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
					req.Header.Add("X-Test", "value2")
					return next.RoundTrip(req)
				})),
			},
			ExpectHeaders: http.Header{
				"X-Test": []string{"value2", "value1"},
			},
		},
	} {
		t.Run(tc.Name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				delete(req.Header, "Accept-Encoding")
				delete(req.Header, "User-Agent")
				assert.Equal(t, tc.ExpectHeaders, req.Header)
			}))
			defer server.Close()
			cfg := tc.Config
			cfg.URIs = []string{server.URL}
			clientParams := []ClientParam{WithConfig(cfg)}
			for _, p := range tc.ClientParams {
				clientParams = append(clientParams, p)
			}

			client, err := NewClient(clientParams...)
			require.NoError(t, err)

			resp, err := client.Get(context.Background(), tc.RequestParams...)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.EqualValues(t, http.StatusOK, resp.StatusCode)
		})
	}
}

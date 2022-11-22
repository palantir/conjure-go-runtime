// Copyright (c) 2019 Palantir Technologies. All rights reserved.
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

package httpclient_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	werror "github.com/palantir/witchcraft-go-error"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorDecoderMiddlewares(t *testing.T) {
	ctx := context.Background()
	verify404 := func(t *testing.T, err error) {
		t.Helper()
		code, ok := httpclient.StatusCodeFromError(err)
		assert.True(t, ok)
		assert.Equal(t, 404, code)
	}
	for _, tc := range []struct {
		name         string
		handler      http.HandlerFunc
		decoderParam httpclient.ClientParam // or nil for default
		verify       func(*testing.T, *url.URL, error)
	}{
		{
			name: "200 OK",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			},
			verify: func(t *testing.T, _ *url.URL, err error) {
				require.NoError(t, err)
			},
		},
		{
			name: "307 no location",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(307)
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				assert.EqualError(t, err, "httpclient request failed: 307 Temporary Redirect")
				code, ok := httpclient.StatusCodeFromError(err)
				assert.True(t, ok)
				assert.Equal(t, 307, code)
				location, ok := httpclient.LocationFromError(err)
				assert.False(t, ok)
				assert.Equal(t, "", location)
			},
		},
		{
			name: "307 with location",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "https://google.com")
				w.WriteHeader(307)
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				assert.NoError(t, err)
			},
		},
		{
			name: "307 with relative location",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/newPath")
				w.WriteHeader(307)
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				assert.EqualError(t, err, "httpclient request failed: 307 Temporary Redirect")
				code, ok := httpclient.StatusCodeFromError(err)
				assert.True(t, ok)
				assert.Equal(t, 307, code)
				location, ok := httpclient.LocationFromError(err)
				assert.True(t, ok)

				// The redirect url is a path relative to original URI
				expected := u.ResolveReference(&url.URL{Path: "/newPath"})
				assert.Equal(t, expected.String(), location)
			},
		},
		{
			name:         "404 DisableRestErrors",
			handler:      http.NotFound,
			decoderParam: httpclient.WithDisableRestErrors(),
			verify: func(t *testing.T, _ *url.URL, err error) {
				require.NoError(t, err)
			},
		},
		{
			name:    "404 default handler",
			handler: http.NotFound,
			verify: func(t *testing.T, u *url.URL, err error) {
				verify404(t, err)
				assert.EqualError(t, err, "httpclient request failed: 404 Not Found")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get", "statusCode": 404}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path", "responseBody": "404 page not found\n"}, unsafeParams)
			},
		},
		{
			name: "404 no body",
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.WriteHeader(404)
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				verify404(t, err)
				assert.EqualError(t, err, "httpclient request failed: 404 Not Found")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get", "statusCode": 404}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path"}, unsafeParams)
			},
		},
		{
			name: "404 plaintext",
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.Header().Set("Content-Type", "text/plain")
				rw.WriteHeader(404)
				_, _ = rw.Write([]byte(`route does not exist`))
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				verify404(t, err)
				assert.EqualError(t, err, "httpclient request failed: 404 Not Found")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get", "statusCode": 404}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path", "responseBody": "route does not exist"}, unsafeParams)
			},
		},
		{
			name: "404 non-conjure json",
			handler: func(rw http.ResponseWriter, req *http.Request) {
				rw.Header().Set("Content-Type", "application/json")
				rw.WriteHeader(404)
				_, _ = rw.Write([]byte(`{"foo":"bar"}`))
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				verify404(t, err)
				assert.EqualError(t, err, "httpclient request failed: 404 Not Found")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get", "statusCode": 404}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path", "responseBody": `{"foo":"bar"}`}, unsafeParams)
			},
		},
		{
			name: "404 conjure",
			handler: func(rw http.ResponseWriter, req *http.Request) {
				errors.WriteErrorResponse(rw, errors.NewNotFound(
					// Safe param will be converted to unsafe because we do not have an error type
					wparams.NewSafeParamStorer(map[string]interface{}{"stringParam": "stringValue"}),
				))
			},
			verify: func(t *testing.T, u *url.URL, err error) {
				verify404(t, err)
				require.Error(t, err)
				conjureErr := errors.GetConjureError(err)
				id := conjureErr.InstanceID()
				assert.NotEmpty(t, id)
				assert.Equal(t, errors.NotFound, conjureErr.Code())
				assert.Equal(t, errors.DefaultNotFound.Name(), conjureErr.Name())

				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get", "errorInstanceId": id, "errorName": "Default:NotFound", "statusCode": 404}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path", "stringParam": "stringValue"}, unsafeParams)
			},
		},
		{
			name:         "404 custom simple decoder",
			handler:      http.NotFound,
			decoderParam: httpclient.WithErrorDecoder(fooErrorDecoder{}),
			verify: func(t *testing.T, u *url.URL, err error) {
				assert.EqualError(t, err, "httpclient request failed: foo error")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get"}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path"}, unsafeParams)
			},
		},
		{
			name:         "404 custom body-reading decoder",
			handler:      http.NotFound,
			decoderParam: httpclient.WithErrorDecoder(bodyReadingErrorDecoder{}),
			verify: func(t *testing.T, u *url.URL, err error) {
				assert.EqualError(t, err, "httpclient request failed: error from body: 404 page not found\n")
				safeParams, unsafeParams := werror.ParamsFromError(err)
				assert.Equal(t, map[string]interface{}{"requestHost": u.Host, "requestMethod": "Get"}, safeParams)
				assert.Equal(t, map[string]interface{}{"requestPath": "/path"}, unsafeParams)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(tc.handler)
			defer ts.Close()
			tsURL, err := url.Parse(ts.URL)
			require.NoError(t, err)

			client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{ts.URL}), httpclient.WithNoProxy(), tc.decoderParam)
			require.NoError(t, err)

			_, err = client.Get(ctx, httpclient.WithPath("/path"))
			tc.verify(t, tsURL, err)
		})
	}
}

type fooErrorDecoder struct{}

func (d fooErrorDecoder) Handles(resp *http.Response) bool {
	return true
}

func (d fooErrorDecoder) DecodeError(resp *http.Response) error {
	return fmt.Errorf("foo error")
}

type bodyReadingErrorDecoder struct{}

func (bodyReadingErrorDecoder) Handles(resp *http.Response) bool {
	return resp.StatusCode == http.StatusNotFound
}

func (bodyReadingErrorDecoder) DecodeError(resp *http.Response) error {
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response body: %v", err)
	}
	return fmt.Errorf("error from body: %s", b)
}

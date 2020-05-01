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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/stretchr/testify/require"
)

func TestNoBaseURIs(t *testing.T) {
	client, err := httpclient.NewClient()
	require.NoError(t, err)

	_, err = client.Do(context.Background(), httpclient.WithRequestMethod("GET"))
	require.Error(t, err)
}

func TestMiddlewareCanReadBody(t *testing.T) {
	unencodedBody := "body"
	encodedBody, err := codecs.Plain.Marshal(unencodedBody)
	require.NoError(t, err)
	bodyIsCorrect := func(body io.ReadCloser) {
		content, err := ioutil.ReadAll(body)
		require.NoError(t, err)
		require.Equal(t, encodedBody, content)
	}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		bodyIsCorrect(req.Body)
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithMiddleware(httpclient.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
			body, err := req.GetBody()
			require.NoError(t, err)
			bodyIsCorrect(body)

			bodyAgain, err := req.GetBody()
			require.NoError(t, err)
			bodyIsCorrect(bodyAgain)

			return next.RoundTrip(req)
		})),
		httpclient.WithBaseURLs([]string{server.URL}),
	)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
	require.NoError(t, err)
	require.NotNil(t, resp)
}

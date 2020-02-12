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
	"testing"

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorsMiddleware(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Run("errors enabled", func(t *testing.T) {
		client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{server.URL}))
		require.NoError(t, err)

		_, err = client.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
		require.Error(t, err)
		status, ok := internal.StatusCodeFromError(err)
		require.True(t, ok)
		require.Equal(t, http.StatusNotFound, status)
	})

	t.Run("errors disabled", func(t *testing.T) {
		client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{server.URL}), httpclient.WithDisableRestErrors())
		require.NoError(t, err)

		_, err = client.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
		require.NoError(t, err)
	})

	t.Run("custom error decoder", func(t *testing.T) {
		client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{server.URL}), httpclient.WithErrorDecoder(fooErrorDecoder{}))
		require.NoError(t, err)

		_, err = client.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
		require.Error(t, err, "foo error")
	})
}

type fooErrorDecoder struct{}

func (d fooErrorDecoder) Handles(resp *http.Response) bool {
	return true
}

func (d fooErrorDecoder) DecodeError(resp *http.Response) error {
	return fmt.Errorf("foo error")
}

func TestErrorDecoderMiddlewareReadsBody(t *testing.T) {
	ctx := context.Background()
	ts := httptest.NewServer(http.NotFoundHandler())
	defer ts.Close()
	t.Run("Client", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
			httpclient.WithErrorDecoder(bodyReadingErrorDecoder{}),
		)
		require.NoError(t, err)
		_, err = client.Get(ctx)
		assert.EqualError(t, err, "httpclient request failed: error from body: 404 page not found\n")
	})
	t.Run("Request", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{ts.URL}),
		)
		require.NoError(t, err)
		_, err = client.Get(ctx, httpclient.WithRequestErrorDecoder(bodyReadingErrorDecoder{}))
		assert.EqualError(t, err, "httpclient request failed: error from body: 404 page not found\n")
	})

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

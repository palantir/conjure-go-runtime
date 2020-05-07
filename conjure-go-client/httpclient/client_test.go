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
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/stretchr/testify/require"
)

func TestNoBaseURIs(t *testing.T) {
	client, err := httpclient.NewClient()
	require.NoError(t, err)

	_, err = client.Do(context.Background(), httpclient.WithRequestMethod("GET"))
	require.Error(t, err)
}

func TestCanReadBodyWithBufferPool(t *testing.T) {
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
		httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
		httpclient.WithBaseURLs([]string{server.URL}),
	)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
	require.NoError(t, err)
	require.NotNil(t, resp)
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

	withMiddleware := httpclient.WithMiddleware(httpclient.MiddlewareFunc(func(req *http.Request, next http.RoundTripper) (*http.Response, error) {
		body, err := req.GetBody()
		require.NoError(t, err)
		bodyIsCorrect(body)

		bodyAgain, err := req.GetBody()
		require.NoError(t, err)
		bodyIsCorrect(bodyAgain)

		return next.RoundTrip(req)
	}))

	t.Run("NoByteBufferPool", func(t *testing.T) {
		client, err := httpclient.NewClient(
			withMiddleware,
			httpclient.WithBaseURLs([]string{server.URL}),
		)
		require.NoError(t, err)
		resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
	t.Run("WithByteBufferPool", func(t *testing.T) {
		client, err := httpclient.NewClient(
			httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
			withMiddleware,
			httpclient.WithBaseURLs([]string{server.URL}),
		)
		require.NoError(t, err)
		resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

func BenchmarkAllocWithBytesBufferPool(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// When making 'count' requests, we expect a client with a bufferpool of size 1 to make roughly 'count'
	// fewer allocations than a client with no bufferpool, due to memory reuse.
	runBench := func(b *testing.B, client httpclient.Client) {
		ctx := context.Background()
		reqBody := httpclient.WithRequestBody("body", codecs.Plain)
		reqMethod := httpclient.WithRequestMethod("GET")
		for _, count := range []int{1, 10, 100} {
			b.Run(fmt.Sprintf("count=%d", count), func(b *testing.B) {
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					for j := 0; j < count; j++ {
						resp, err := client.Do(ctx, reqBody, reqMethod)
						require.NoError(b, err)
						require.NotNil(b, resp)
					}
				}
			})
		}
	}
	b.Run("NoByteBufferPool", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURLs([]string{server.URL}),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("WithByteBufferPool", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
			httpclient.WithBaseURLs([]string{server.URL}),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
}

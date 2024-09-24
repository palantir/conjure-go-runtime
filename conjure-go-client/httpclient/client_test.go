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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/pkg/bytesbuffers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoBaseURIs(t *testing.T) {
	client, err := httpclient.NewClient()
	require.EqualError(t, err, "httpclient URLs must be set in configuration or by constructor param")
	require.Nil(t, client)
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
		httpclient.WithBaseURL(server.URL),
	)
	require.NoError(t, err)

	resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCanUseRelocationURI(t *testing.T) {
	respBody := map[string]string{"key-1": "value-1"}

	relocationServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/newPath":
			rw.WriteHeader(200)
			assert.NoError(t, codecs.JSON.Encode(rw, respBody))
		}
	}))
	defer relocationServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/oldPath":
			rw.Header().Add("Location", relocationServer.URL+"/newPath")
			rw.WriteHeader(307)
		}
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
		httpclient.WithBaseURL(server.URL),
	)
	assert.NoError(t, err)

	var actualRespBody map[string]string
	resp, err := client.Do(context.Background(),
		httpclient.WithRequestMethod("GET"),
		httpclient.WithPath("/oldPath"),
		httpclient.WithJSONResponse(&actualRespBody),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, resp.StatusCode, 200)
	assert.Equal(t, respBody, actualRespBody)
}

func TestCanUseSimpleRelocationURI(t *testing.T) {
	respBody := map[string]string{"key-1": "value-1"}

	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/newPath":
			rw.WriteHeader(200)
			assert.NoError(t, codecs.JSON.Encode(rw, respBody))
		case "/oldPath":
			rw.Header().Add("Location", "/newPath")
			rw.WriteHeader(307)
		}
	}))
	defer server.Close()

	client, err := httpclient.NewClient(
		httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
		httpclient.WithBaseURL(server.URL),
	)
	assert.NoError(t, err)

	var actualRespBody map[string]string
	resp, err := client.Do(context.Background(),
		httpclient.WithRequestMethod("GET"),
		httpclient.WithPath("/oldPath"),
		httpclient.WithJSONResponse(&actualRespBody),
	)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, resp.StatusCode, 200)
	assert.Equal(t, respBody, actualRespBody)
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
			httpclient.WithBaseURL(server.URL),
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
			httpclient.WithBaseURL(server.URL),
		)
		require.NoError(t, err)
		resp, err := client.Do(context.Background(), httpclient.WithRequestBody(unencodedBody, codecs.Plain), httpclient.WithRequestMethod("GET"))
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

func TestTimeouts(t *testing.T) {
	// General timeout to avoid hanging/slowness asserted at the end of the test.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	timeoutServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		timeout, err := time.ParseDuration(req.URL.Query().Get("timeout"))
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		t.Logf("sleeping for %v", timeout)
		select {
		case <-time.After(timeout):
			t.Logf("request completed")
			rw.WriteHeader(http.StatusOK)
		case <-req.Context().Done():
			t.Logf("request canceled: %v", req.Context().Err())
			rw.WriteHeader(http.StatusRequestTimeout)
		case <-ctx.Done():
			t.Logf("test canceled: %v", ctx.Err())
			rw.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer timeoutServer.Close()

	for _, tt := range []struct {
		Name           string
		ServerTimeout  time.Duration
		ClientTimeout  time.Duration
		RequestTimeout time.Duration
		ExpectTimeout  bool
		ExpectStatus   int
	}{
		{
			Name:           "short request less than client timeout",
			ServerTimeout:  time.Millisecond,
			ClientTimeout:  time.Second,
			RequestTimeout: 0,
			ExpectTimeout:  false,
			ExpectStatus:   http.StatusOK,
		},
		{
			Name:           "short request less than request timeout",
			ServerTimeout:  time.Millisecond,
			ClientTimeout:  0,
			RequestTimeout: time.Second,
			ExpectTimeout:  false,
			ExpectStatus:   http.StatusOK,
		},
		{
			Name:           "slow request longer than client timeout",
			ServerTimeout:  time.Second,
			ClientTimeout:  time.Millisecond,
			RequestTimeout: 0,
			ExpectTimeout:  true,
			ExpectStatus:   0,
		},
		{
			Name:           "slow request longer than request timeout",
			ServerTimeout:  time.Second,
			ClientTimeout:  0,
			RequestTimeout: time.Millisecond,
			ExpectTimeout:  true,
			ExpectStatus:   0,
		},
	} {
		t.Run(tt.Name, func(t *testing.T) {
			clientParams := []httpclient.ClientParam{
				httpclient.WithBaseURLs([]string{timeoutServer.URL}),
				httpclient.WithMaxRetries(0),
			}
			if tt.ClientTimeout > 0 {
				clientParams = append(clientParams, httpclient.WithHTTPTimeout(tt.ClientTimeout))
			}
			client, err := httpclient.NewClient(clientParams...)
			require.NoError(t, err)
			requestParams := []httpclient.RequestParam{
				httpclient.WithQueryValues(map[string][]string{"timeout": {tt.ServerTimeout.String()}}),
			}
			if tt.RequestTimeout > 0 {
				requestParams = append(requestParams, httpclient.WithRequestTimeout(tt.RequestTimeout))
			}
			resp, err := client.Get(ctx, requestParams...)
			if tt.ExpectTimeout {
				require.Error(t, err)
				require.Nil(t, resp)
				var netErr net.Error
				assert.True(t, errors.As(err, &netErr), "error should be a net.Error: %v", err)
				assert.True(t, netErr.Timeout(), "error should be a timeout: %v", err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, tt.ExpectStatus, resp.StatusCode)
			}
		})
	}

	require.NoError(t, ctx.Err(), "context should not be canceled: test did not complete in expected time")
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
			httpclient.WithBaseURL(server.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("WithByteBufferPool", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBytesBufferPool(bytesbuffers.NewSizedPool(1, 10)),
			httpclient.WithBaseURL(server.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
}

func BenchmarkUnavailableURIs(b *testing.B) {
	server1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server1.Close()
	server2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server2.Close()
	server3 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server3.Close()
	server4 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server4.Close()
	unavailableServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unavailableServer.Close()
	unstartedServer := httptest.NewUnstartedServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))

	runBench := func(b *testing.B, client httpclient.Client) {
		ctx := context.Background()
		reqBody := httpclient.WithRequestBody("body", codecs.Plain)
		reqMethod := httpclient.WithRequestMethod("GET")
		for _, count := range []int{10, 100, 1000} {
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
	b.Run("OneAvailableServer", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("FourAvailableServers", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL, server2.URL, server3.URL, server4.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("OneOutOfFourUnavailableServers", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL, server2.URL, server3.URL, unavailableServer.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("OneOutOfThreeUnavailableServers", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL, server2.URL, unavailableServer.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("OneOutOfTwoUnavailableServers", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL, unavailableServer.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
	b.Run("OneOutOfTwoUnstartedServers", func(b *testing.B) {
		client, err := httpclient.NewClient(
			httpclient.WithBaseURL(server1.URL, unstartedServer.URL),
		)
		require.NoError(b, err)
		runBench(b, client)
	})
}

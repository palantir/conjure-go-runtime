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
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient/internal"
	"github.com/palantir/pkg/httpserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFailover503(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		if n == 3 {
			rw.WriteHeader(http.StatusOK)
		} else {
			rw.WriteHeader(http.StatusServiceUnavailable)
		}
	})

	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	s3 := httptest.NewServer(handler)

	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL, s3.URL}))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
}

func TestFailover429(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		if n == 3 {
			rw.WriteHeader(http.StatusOK)
			_, err := rw.Write([]byte("body"))
			require.NoError(t, err)
		} else {
			rw.WriteHeader(http.StatusTooManyRequests)
			_, err := rw.Write([]byte("body"))
			require.NoError(t, err)
		}
	})

	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	s3 := httptest.NewServer(handler)

	backoff := 5 * time.Millisecond
	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL, s3.URL}), WithInitialBackoff(backoff), WithMaxBackoff(backoff))
	require.NoError(t, err)

	timer := time.NewTimer(backoff)
	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	select {
	case <-timer.C:
		break
	default:
		t.Error("Timer was not complete, back-off did not appear to occur")
	}
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
}

func TestFailover200(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		rw.WriteHeader(http.StatusOK)
	})

	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	s3 := httptest.NewServer(handler)
	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL, s3.URL}))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.Nil(t, err)
	assert.Equal(t, 1, n)
}

func TestFailoverEverythingDown(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		rw.WriteHeader(http.StatusServiceUnavailable)
	})

	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	s3 := httptest.NewServer(handler)
	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL, s3.URL}), WithMaxRetries(5))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.NotNil(t, err)
	assert.Equal(t, 6, n)
}

func TestBackoffSingleURL(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		if n < 3 {
			rw.WriteHeader(http.StatusServiceUnavailable)
		} else {
			rw.WriteHeader(http.StatusOK)
		}
	})
	s1 := httptest.NewServer(handler)
	cli, err := NewClient(WithBaseURLs([]string{s1.URL}), WithMaxRetries(4))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.Nil(t, err)
	assert.Equal(t, 3, n)
}

func TestFailoverOtherURL(t *testing.T) {
	didHitS1, didHitS2 := false, false

	s1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		didHitS1 = true
		rw.WriteHeader(http.StatusOK)
	}))

	s2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		didHitS2 = true
		rw.Header()["Location"] = []string{s1.URL}
		rw.WriteHeader(http.StatusPermanentRedirect)
	}))

	s3 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		didHitS2 = true
		rw.Header()["Location"] = []string{s1.URL}
		rw.WriteHeader(http.StatusPermanentRedirect)
	}))

	cli, err := NewClient(WithBaseURLs([]string{s2.URL, s3.URL}))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.NoError(t, err)
	assert.True(t, didHitS2)
	assert.True(t, didHitS1)
}

func TestFailoverDistribution(t *testing.T) {
	requests := 100
	serverCount := 3
	totalHits := 0
	serverHits := make([]int, serverCount)
	servers := make([]*httptest.Server, serverCount)
	urls := make([]string, serverCount)
	for i := 0; i < serverCount; i++ {
		serverIndex := i
		servers[serverIndex] = httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			serverHits[serverIndex]++
			totalHits++
			rw.WriteHeader(http.StatusOK)
		}))
		urls[serverIndex] = servers[serverIndex].URL
	}
	cli, err := NewClient(WithBaseURLs(urls))
	require.NoError(t, err)

	// Disable one server
	servers[0].Close()
	for i := 0; i < requests; i++ {
		_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
		assert.NoError(t, err)
	}
	assert.Equal(t, requests, totalHits)
	for i := 0; i < serverCount; i++ {
		// Validate that requests are evenly distributed across servers
		assert.True(t, serverHits[i] < 2*requests/serverCount)
	}
}

func TestSleep(t *testing.T) {
	n := 0
	handler := http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		n++
		switch n {
		case 1:
			rw.Header()["Retry-After"] = []string{"30"}
			rw.WriteHeader(internal.StatusCodeThrottle)
			return
		case 2:
			rw.WriteHeader(http.StatusOK)
		}
	})

	s1 := httptest.NewServer(handler)
	s2 := httptest.NewServer(handler)
	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL}))
	require.NoError(t, err)

	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestRoundRobin(t *testing.T) {
	requestsPerServer := make([]int, 3)
	getHandler := func(i int) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			requestsPerServer[i]++
			rw.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	s0 := httptest.NewServer(getHandler(0))
	s1 := httptest.NewServer(getHandler(1))
	s2 := httptest.NewServer(getHandler(2))
	cli, err := NewClient(WithBaseURLs([]string{s0.URL, s1.URL, s2.URL}), WithMaxRetries(5))
	require.NoError(t, err)
	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.Error(t, err)
	assert.Equal(t, []int{2, 2, 2}, requestsPerServer)
}

func TestFailover_ConnectionRefused(t *testing.T) {
	port1, err := httpserver.AvailablePort()
	require.NoError(t, err)
	url1 := fmt.Sprintf("http://localhost:%d/", port1)
	port2, err := httpserver.AvailablePort()
	require.NoError(t, err)
	url2 := fmt.Sprintf("http://localhost:%d/", port2)

	s1 := httptest.NewServer(http.NotFoundHandler())
	defer s1.Close()

	cli, err := NewClient(WithBaseURLs([]string{url1, url2, url1, url2, s1.URL}), WithDisableRestErrors())
	require.NoError(t, err)

	_, err = cli.Get(context.Background())
	assert.NoError(t, err)
}

func TestFailover_NoHost(t *testing.T) {
	port1, err := httpserver.AvailablePort()
	require.NoError(t, err)
	url1 := fmt.Sprintf("http://not-a-hostname-1:%d/", port1)
	port2, err := httpserver.AvailablePort()
	require.NoError(t, err)
	url2 := fmt.Sprintf("http://not-a-hostname-2:%d/", port2)

	s1 := httptest.NewServer(http.NotFoundHandler())
	defer s1.Close()

	cli, err := NewClient(WithBaseURLs([]string{url1, url2, url1, url2, s1.URL}), WithDisableRestErrors())
	require.NoError(t, err)

	_, err = cli.Get(context.Background())
	assert.NoError(t, err)
}

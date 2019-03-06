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

	"github.com/palantir/conjure-go-runtime/conjure-go-client/httpclient/internal"
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
	cli, err := NewClient(WithBaseURLs([]string{s1.URL, s2.URL, s3.URL}))
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
	requestsPerSever := make([]int, 3)
	getHandler := func(i int) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			requestsPerSever[i]++
			rw.WriteHeader(http.StatusServiceUnavailable)
		})
	}
	s0 := httptest.NewServer(getHandler(0))
	s1 := httptest.NewServer(getHandler(1))
	s2 := httptest.NewServer(getHandler(2))
	cli, err := NewClient(WithBaseURLs([]string{s0.URL, s1.URL, s2.URL}))
	require.NoError(t, err)
	_, err = cli.Do(context.Background(), WithRequestMethod("GET"))
	assert.Error(t, err)
	assert.Equal(t, []int{2, 2, 2}, requestsPerSever)
}

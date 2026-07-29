// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBalancedScorerRandomizesWithNoneInflight(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewBalancedURIScoringMiddleware(uris, func() int64 { return 0 })
	scoredUris := scorer.GetURIsInOrderOfIncreasingScore()
	assert.ElementsMatch(t, scoredUris, uris)
	assert.NotEqual(t, scoredUris, uris)
}

func TestBalancedScoring(t *testing.T) {
	server200 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server200.Close()
	server429 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server429.Close()
	server503 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server503.Close()
	uris := []string{server503.URL, server429.URL, server200.URL}
	scorer := NewBalancedURIScoringMiddleware(uris, func() int64 { return 0 })
	for _, server := range []*httptest.Server{server200, server429, server503} {
		for range 10 {
			req, err := http.NewRequest("GET", server.URL, nil)
			assert.NoError(t, err)
			_, err = scorer.RoundTrip(req, server.Client().Transport)
			assert.NoError(t, err)
		}
	}
	scoredUris := scorer.GetURIsInOrderOfIncreasingScore()
	assert.Equal(t, []string{server200.URL, server429.URL, server503.URL}, scoredUris)
}

func TestBalancedScoringTracksInflightRequests(t *testing.T) {
	const uri = "https://example.com"
	scorer := NewBalancedURIScoringMiddleware([]string{uri}, func() int64 { return 0 }).(*balancedScorer)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	transport := roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		once.Do(func() {
			close(started)
		})
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
		}, nil
	})
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	require.NoError(t, err)
	go func() {
		defer close(done)
		_, _ = scorer.RoundTrip(req, transport)
	}()
	<-started

	assert.Equal(t, int32(1), scorer.uriInfos[uri].computeScore())
	close(release)
	<-done
	assert.Equal(t, int32(0), scorer.uriInfos[uri].computeScore())
}

// TestBalancedScoring_InflightRequestsAffectsScore asserts that an in-flight request against uriA
// causes uriA to rank worse than an idle uriB. It should never rank equal to or ahead of an idle host.
func TestBalancedScoring_InflightRequestsAffectsScore(t *testing.T) {
	uriA := "https://a.example.com"
	uriB := "https://b.example.com"
	scorer := NewBalancedURIScoringMiddleware([]string{uriA, uriB}, func() int64 { return 0 })

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var once sync.Once
	slowTransport := roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
		once.Do(func() {
			close(started)
		})
		<-release
		return &http.Response{
			StatusCode: http.StatusOK,
		}, nil
	})

	reqA, _ := http.NewRequest(http.MethodGet, uriA, nil)
	go func() {
		defer close(done)
		_, _ = scorer.RoundTrip(reqA, slowTransport)
	}()
	<-started // wait until the request to uriA is actually in flight

	order := scorer.GetURIsInOrderOfIncreasingScore()
	assert.Equal(t, uriB, order[0], "expected idle uriB should rank ahead of the in-flight uriA")

	close(release)
	<-done
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

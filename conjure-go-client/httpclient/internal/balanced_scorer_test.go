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
	"testing"

	"github.com/stretchr/testify/assert"
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
		for i := 0; i < 10; i++ {
			req, err := http.NewRequest("GET", server.URL, nil)
			assert.NoError(t, err)
			_, err = scorer.RoundTrip(req, server.Client().Transport)
			assert.NoError(t, err)
		}
	}
	scoredUris := scorer.GetURIsInOrderOfIncreasingScore()
	assert.Equal(t, []string{server200.URL, server429.URL, server503.URL}, scoredUris)
}

// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpc

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripperFn func(*http.Request) (*http.Response, error)

func (f roundTripperFn) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestBalancedSelector_RandomizesWithNoneInflight(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	sel := newBalancedSelector(uris, func() int64 { return 0 })
	ordered := sel.BaseURLs()
	assert.ElementsMatch(t, ordered, uris)
	assert.NotEqual(t, uris, ordered)
}

func TestBalancedSelector_ScoresByFailures(t *testing.T) {
	server200 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer server200.Close()
	server429 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server429.Close()
	server503 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server503.Close()

	uris := []string{server503.URL, server429.URL, server200.URL}
	sel := newBalancedSelector(uris, func() int64 { return 0 })
	for _, server := range []*httptest.Server{server200, server429, server503} {
		for range 10 {
			req, err := http.NewRequest(http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			_, err = sel.RoundTrip(req, server.Client().Transport)
			require.NoError(t, err)
		}
	}
	// 200 has no failures, 429 a small weight, 503 a full weight.
	assert.Equal(t, []string{server200.URL, server429.URL, server503.URL}, sel.BaseURLs())
}

func TestBalancedSelector_TracksInflight(t *testing.T) {
	const (
		busyURI = "http://busy.example.com"
		idleURI = "http://idle.example.com"
	)
	sel := newBalancedSelector([]string{busyURI, idleURI}, func() int64 { return 0 })
	req, err := http.NewRequest(http.MethodGet, busyURI+"/test", nil)
	require.NoError(t, err)

	started := make(chan struct{})
	release := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		_, err := sel.RoundTrip(req, roundTripperFn(func(*http.Request) (*http.Response, error) {
			close(started)
			<-release
			return &http.Response{StatusCode: http.StatusOK}, nil
		}))
		errCh <- err
	}()

	<-started
	assert.Equal(t, []string{idleURI, busyURI}, sel.BaseURLs())
	close(release)
	require.NoError(t, <-errCh)
}

func TestRandomSelector_Randomizes(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	var counter atomic.Int64
	sel := &randomSelector{uris: uris, nanoClock: func() int64 { return counter.Add(1) }}
	first := sel.BaseURLs()
	second := sel.BaseURLs()
	assert.ElementsMatch(t, first, second)
	assert.NotEqual(t, first, second)
}

func TestDecayReservoir_DecayToZero(t *testing.T) {
	now := int64(0)
	r := newDecayReservoir(func() int64 { return now }, 10)
	assert.InDelta(t, 0.0, r.Get(), 0.001)
	r.Update(1)
	assert.InDelta(t, 1.0, r.Get(), 0.001)
	now = 300000
	assert.InDelta(t, 0.0, r.Get(), 0.001)
}

func TestDecayReservoir_DecayByHalf(t *testing.T) {
	now := int64(0)
	r := newDecayReservoir(func() int64 { return now }, 10)
	r.Update(2)
	assert.InDelta(t, 2.0, r.Get(), 0.001)
	now = 10
	assert.InDelta(t, 1.0, r.Get(), 0.001)
	now = 20
	assert.InDelta(t, 0.5, r.Get(), 0.001)
}

func TestDecayReservoir_IntermediateDecay(t *testing.T) {
	now := int64(0)
	r := newDecayReservoir(func() int64 { return now }, 10)
	r.Update(100)
	assert.InDelta(t, 100.0, r.Get(), 0.001)
	now = 2
	assert.Less(t, r.Get(), 100.0)
	assert.Greater(t, r.Get(), 50.0)
	now = 10
	assert.InDelta(t, 50.0, r.Get(), 0.001)
}
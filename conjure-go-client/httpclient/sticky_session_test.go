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

package httpclient_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient"
	"github.com/palantir/pkg/refreshable/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStickySession_FirstRequestGetsFailover verifies that the first request on a sticky session
// before any pin exists, gets the client's normal cross-host failover.
func TestStickySession_FirstRequestGetsFailover(t *testing.T) {
	var (
		ctx      = t.Context()
		badHits  atomic.Int32
		goodHits atomic.Int32
	)
	bad := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		badHits.Add(1)
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		goodHits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer good.Close()

	client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{bad.URL, good.URL}))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(client)
	require.NoError(t, err)

	// bad/good requests are randomized on the first attempt, but a request to bad should always fail over to good.
	_, err = sticky.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	badHitsAfterFirstCall := badHits.Load()
	assert.LessOrEqual(t, badHitsAfterFirstCall, int32(1))
	assert.Equal(t, int32(1), goodHits.Load())

	// Once pinned, every subsequent call must land on the good server
	for range 5 {
		_, err = sticky.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
		require.NoError(t, err)
	}
	assert.Equal(t, badHitsAfterFirstCall, badHits.Load())
	assert.Equal(t, int32(6), goodHits.Load())
}

// TestStickySession_NoRetryOncePinned verifies that once a sticky session has a pin, a failing
// request makes exactly one attempt with no retries.
func TestStickySession_NoRetryOncePinned(t *testing.T) {
	var hits atomic.Int32
	pinned := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer pinned.Close()

	client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{pinned.URL}))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(client)
	require.NoError(t, err)

	// First call succeeds, establishing the pin.
	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.Equal(t, int32(1), hits.Load())

	// Now the pinned host starts failing.
	pinned.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusServiceUnavailable)
	})
	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	assert.Error(t, err)
	assert.Equal(t, int32(2), hits.Load())
}

// TestStickySession_PinnedURIRemovedFailsFast verifies that once a pinned URI is no longer part of
// the client's current refreshable URI set, the session fails fast rather than silently repinning.
func TestStickySession_PinnedURIRemovedFailsFast(t *testing.T) {
	var hits atomic.Int32
	s1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer s2.Close()

	uris := refreshable.New([]string{s1.URL})
	client, err := httpclient.NewClientFromRefreshableConfig(
		t.Context(),
		refreshable.New(httpclient.ClientConfig{}),
		httpclient.WithRefreshableBaseURLs(uris))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(client)
	require.NoError(t, err)

	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.Equal(t, int32(1), hits.Load())

	// Remove the pinned URI (s1) from the URI list.
	uris.Update([]string{s2.URL})

	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	assert.ErrorIs(t, err, httpclient.ErrStickyPinInvalidated)
	assert.Equal(t, int32(1), hits.Load())
}

// TestStickySession_ConcurrentCallsConvergeOnSameHost verifies that once a sticky session is pinned,
// many concurrent goroutines calling Do all land on the same host.
func TestStickySession_ConcurrentCallsConvergeOnSameHost(t *testing.T) {
	var (
		ctx    = t.Context()
		s1Hits atomic.Int32
		s2Hits atomic.Int32
	)
	s1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		s1Hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer s1.Close()
	s2 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		s2Hits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer s2.Close()

	client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{s1.URL, s2.URL}))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(client)
	require.NoError(t, err)

	// Establish the pin with a single call before starting concurrent callers
	_, err = sticky.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)

	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			_, doErr := sticky.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
			assert.NoError(t, doErr)
		}()
	}
	wg.Wait()

	total := s1Hits.Load() + s2Hits.Load()
	assert.Equal(t, int32(n+1), total)
	assert.True(t, s1Hits.Load() == 0 || s2Hits.Load() == 0, "all concurrent calls must land on the single pinned host")
}

// TestStickySession_SharedHealthState verifies that a sticky session maintains shared health scoring.
// A failure observed through one session should always affect the ranking of a later one, since they share the same URI scorer.
func TestStickySession_SharedHealthState(t *testing.T) {
	var (
		ctx              = t.Context()
		firstRequestDone atomic.Bool
		aHits, bHits     atomic.Int32
		aFailed, bFailed atomic.Bool
	)
	fail := func(failed *atomic.Bool, rw http.ResponseWriter) {
		if !firstRequestDone.Swap(true) {
			failed.Store(true)
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.WriteHeader(http.StatusOK)
	}
	serverA := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		aHits.Add(1)
		fail(&aFailed, rw)
	}))
	defer serverA.Close()
	serverB := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		bHits.Add(1)
		fail(&bFailed, rw)
	}))
	defer serverB.Close()

	client, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{serverA.URL, serverB.URL}))
	require.NoError(t, err)

	// Whichever server the scorer sends this first request to fails and the client must fail over to the other server and succeed.
	_, err = client.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.True(t, aFailed.Load() != bFailed.Load(), "exactly one server must have received the forced first failure")

	badHits := &aHits
	if bFailed.Load() {
		badHits = &bHits
	}
	hitsBeforeSticky := badHits.Load()

	// A fresh sticky session from the same client and the same scorer should avoid whichever server just failed.
	sticky, err := httpclient.NewStickySession(client)
	require.NoError(t, err)
	_, err = sticky.Do(ctx, httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	assert.Equal(t, hitsBeforeSticky, badHits.Load(), "sticky session's first request should have avoided the host that just failed")
}

// TestStickySession_UnsupportedClientReturnsError verifies that NewStickySession fails when called
// on a Client that isn't the concrete CGR defined [httpclient.Client].
func TestStickySession_UnsupportedClientReturnsError(t *testing.T) {
	s1 := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	defer s1.Close()
	cli, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{s1.URL}))
	require.NoError(t, err)

	type passthroughClient struct {
		httpclient.Client
	}

	decorator := &passthroughClient{Client: cli}
	sticky, err := httpclient.NewStickySession(decorator)
	assert.Nil(t, sticky)
	assert.ErrorIs(t, err, httpclient.ErrStickySessionUnsupported)
}

// TestStickySession_RedirectNotFollowedOncePinned verifies that once a sticky session is pinned, a
// 307/308 response from the pinned host is surfaced as a failure rather than followed.
func TestStickySession_RedirectNotFollowedOncePinned(t *testing.T) {
	var (
		pinnedHits atomic.Int32
		otherHits  atomic.Int32
	)
	other := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		otherHits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer other.Close()
	pinned := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		pinnedHits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer pinned.Close()

	cli, err := httpclient.NewClient(httpclient.WithBaseURLs([]string{pinned.URL}))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(cli)
	require.NoError(t, err)

	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	require.NoError(t, err)
	require.Equal(t, int32(1), pinnedHits.Load())

	// Now the pinned host starts issuing a redirect to other but because the session is pinned, this must never be followed.
	pinned.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		pinnedHits.Add(1)
		rw.Header().Set("Location", other.URL)
		rw.WriteHeader(http.StatusPermanentRedirect)
	})

	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	assert.Error(t, err)
	assert.Equal(t, int32(2), pinnedHits.Load())
	assert.Equal(t, int32(0), otherHits.Load(), "redirect must never be followed once a session is pinned")
}

// TestStickySession_InFlightRequestFinishesAfterURIRemoval verifies that removing a session's pinned
// URI from a refreshable URI list mid-flight does not disturb a request already in progress against it.
func TestStickySession_InFlightRequestFinishesAfterURIRemoval(t *testing.T) {
	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	var pinnedHits, newHits atomic.Int32

	pinned := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		pinnedHits.Add(1)
		close(requestStarted)
		<-releaseRequest
		rw.WriteHeader(http.StatusOK)
	}))
	defer pinned.Close()
	replacement := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		newHits.Add(1)
		rw.WriteHeader(http.StatusOK)
	}))
	defer replacement.Close()

	uris := refreshable.New([]string{pinned.URL})
	cli, err := httpclient.NewClientFromRefreshableConfig(t.Context(), refreshable.New(httpclient.ClientConfig{}),
		httpclient.WithRefreshableBaseURLs(uris))
	require.NoError(t, err)
	sticky, err := httpclient.NewStickySession(cli)
	require.NoError(t, err)

	callErr := make(chan error, 1)
	go func() {
		_, doErr := sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
		callErr <- doErr
	}()
	<-requestStarted

	// Remove the pinned URI while the request above is still in flight against it.
	uris.Update([]string{replacement.URL})
	close(releaseRequest)
	require.NoError(t, <-callErr, "an in-flight request must be allowed to finish even if its URI is removed mid-flight")

	// The next call observes the removal and fails fast, never touching the replacement
	_, err = sticky.Do(t.Context(), httpclient.WithRequestMethod(http.MethodGet))
	assert.ErrorIs(t, err, httpclient.ErrStickyPinInvalidated)
	assert.Equal(t, int32(1), pinnedHits.Load())
	assert.Equal(t, int32(0), newHits.Load())
}

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
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/palantir/pkg/refreshable/v2"
)

// URLSelector chooses the base URLs to try for a request, in preferred order,
// and observes attempt outcomes to inform future ordering. It is also a
// [Middleware]: a client built via [Builder] wraps each attempt with the
// selector so it can track in-flight load and recent failures per base URL.
//
// Set a custom selector with [Builder.SetURLSelector]; the built-in
// [BalancedURLSelector] (default) and [RandomURLSelector] cover the common cases.
type URLSelector interface {
	// BaseURLs returns the configured base URLs in the order they should be
	// tried for the next request.
	BaseURLs() []string
	Middleware
}

// BalancedURLSelector scores base URLs by fewest in-flight requests and fewest
// recent failures, routing away from slow or erroring hosts. It is the default.
func BalancedURLSelector(uris []string) URLSelector {
	return newBalancedSelector(uris, nanoClock)
}

// RandomURLSelector orders base URLs uniformly at random per request.
func RandomURLSelector(uris []string) URLSelector {
	return &randomSelector{uris: uris, nanoClock: nanoClock}
}

func nanoClock() int64 { return time.Now().UnixNano() }

const (
	selectorFailureWeight = 10.0
	selectorFailureMemory = 30 * time.Second
)

// balancedSelector tracks in-flight requests and recent failures per base URL.
// URIs are scored by fewest in-flight requests plus recent errors, where client
// errors count as 1/100 of a failure, and server errors / QoS responses as a
// full failure, decayed with a 30s half-life.
//
// Based on Dialogue's BalancedScoreTracker:
// https://github.com/palantir/dialogue/blob/develop/dialogue-core/src/main/java/com/palantir/dialogue/core/BalancedScoreTracker.java
type balancedSelector struct {
	uriInfos map[string]*uriInfo
}

type uriInfo struct {
	inflight       int32
	recentFailures *decayReservoir
}

func newBalancedSelector(uris []string, nanoClock func() int64) URLSelector {
	uriInfos := make(map[string]*uriInfo, len(uris))
	for _, uri := range uris {
		uriInfos[uri] = &uriInfo{
			recentFailures: newDecayReservoir(nanoClock, selectorFailureMemory),
		}
	}
	return &balancedSelector{uriInfos: uriInfos}
}

func (s *balancedSelector) BaseURLs() []string {
	uris := make([]string, 0, len(s.uriInfos))
	scores := make(map[string]int32, len(s.uriInfos))
	for uri, info := range s.uriInfos {
		uris = append(uris, uri)
		scores[uri] = info.computeScore()
	}
	// Pre-shuffle to avoid overloading the first URI when no requests are in-flight.
	rand.Shuffle(len(uris), func(i, j int) {
		uris[i], uris[j] = uris[j], uris[i]
	})
	sort.Slice(uris, func(i, j int) bool {
		return scores[uris[i]] < scores[uris[j]]
	})
	return uris
}

func (s *balancedSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	baseURI := baseURIOf(req.URL)
	info, foundInfo := s.uriInfos[baseURI]
	if foundInfo {
		atomic.AddInt32(&info.inflight, 1)
		defer atomic.AddInt32(&info.inflight, -1)
	}
	resp, err := next.RoundTrip(req)
	if resp == nil || err != nil {
		if foundInfo {
			info.recentFailures.Update(selectorFailureWeight)
		}
		return nil, err
	}
	if foundInfo {
		switch statusCode := resp.StatusCode; {
		case statusCode == http.StatusPermanentRedirect || statusCode == http.StatusServiceUnavailable || statusCode/100 == 5:
			info.recentFailures.Update(selectorFailureWeight)
		case statusCode/100 == 4:
			info.recentFailures.Update(selectorFailureWeight / 100)
		}
	}
	return resp, nil
}

func (i *uriInfo) computeScore() int32 {
	return atomic.LoadInt32(&i.inflight) + int32(math.Round(i.recentFailures.Get()))
}

// baseURIOf reduces a request URL to its scheme/host identity for scorer lookup.
func baseURIOf(u *url.URL) string {
	base := url.URL{Scheme: u.Scheme, Opaque: u.Opaque, User: u.User, Host: u.Host}
	return base.String()
}

// randomSelector shuffles the base URLs per request and no-ops on round trips.
type randomSelector struct {
	uris      []string
	nanoClock func() int64
}

func (s *randomSelector) BaseURLs() []string {
	uris := make([]string, len(s.uris))
	copy(uris, s.uris)
	rand.New(rand.NewSource(s.nanoClock())).Shuffle(len(uris), func(i, j int) {
		uris[i], uris[j] = uris[j], uris[i]
	})
	return uris
}

func (s *randomSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

// refreshableSelector rebuilds the underlying selector whenever the base URLs change.
type refreshableSelector struct {
	refreshable.Refreshable[URLSelector]
}

func newRefreshableSelector(uris refreshable.Refreshable[[]string], factory func([]string) URLSelector) URLSelector {
	mapped, _ := refreshable.Map(uris, factory)
	return refreshableSelector{mapped}
}

func (r refreshableSelector) BaseURLs() []string {
	return r.Current().BaseURLs()
}

func (r refreshableSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return r.Current().RoundTrip(req, next)
}

const decaysPerHalfLife = 10

var decayFactor = math.Pow(0.5, 1.0/decaysPerHalfLife)

// decayReservoir is a coarse exponential-decay accumulator used to age out
// recent failures with a fixed half-life.
type decayReservoir struct {
	lastDecay                int64
	nanoClock                func() int64
	decayIntervalNanoseconds int64
	mu                       sync.Mutex
	value                    float64
}

func newDecayReservoir(nanoClock func() int64, halfLife time.Duration) *decayReservoir {
	return &decayReservoir{
		lastDecay:                nanoClock(),
		nanoClock:                nanoClock,
		decayIntervalNanoseconds: halfLife.Nanoseconds() / decaysPerHalfLife,
	}
}

func (r *decayReservoir) Update(updates float64) {
	r.decayIfNecessary()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value += updates
}

func (r *decayReservoir) Get() float64 {
	r.decayIfNecessary()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value
}

func (r *decayReservoir) decayIfNecessary() {
	now := r.nanoClock()
	lastDecaySnapshot := r.lastDecay
	decays := (now - lastDecaySnapshot) / r.decayIntervalNanoseconds
	// If the CAS fails another goroutine is performing the decay instead.
	if decays > 0 && atomic.CompareAndSwapInt64(&r.lastDecay, lastDecaySnapshot, lastDecaySnapshot+decays*r.decayIntervalNanoseconds) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.value *= math.Pow(decayFactor, float64(decays))
	}
}
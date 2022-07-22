// Copyright (c) 2022 Palantir Technologies. All rights reserved.
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
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	werror "github.com/palantir/witchcraft-go-error"
)

const (
	failureWeight = 10.0
	failureMemory = 30 * time.Second
)

// NewBalancedURISelector returns URI scoring middleware that tracks in-flight requests and recent failures
// for each URI configured on an HTTP client. URIs are scored based on fewest in-flight requests and recent errors,
// where client errors are weighted the same as 1/10 of an in-flight request, server errors are weighted as 10
// in-flight requests, and errors are decayed using exponential decay with a half-life of 30 seconds.
//
// This implementation is based on Dialogue's BalancedScoreTracker:
// https://github.com/palantir/dialogue/blob/develop/dialogue-core/src/main/java/com/palantir/dialogue/core/BalancedScoreTracker.java
func NewBalancedURISelector(nanoClock func() int64) URISelector {
	return &balancedSelector{
		nanoClock: nanoClock,
	}
}

type balancedSelector struct {
	sync.Mutex

	nanoClock func() int64
	uriInfos  map[string]uriInfo
}

// Select implements estransport.Selector interface
func (s *balancedSelector) Select(uris []string, _ http.Header) (string, error) {
	s.Lock()
	defer s.Unlock()

	s.updateURIs(uris)
	return s.next()
}

func (s *balancedSelector) updateURIs(uris []string) {
	uriInfos := make(map[string]uriInfo, len(uris))
	for _, uri := range uris {
		if exisiting, ok := s.uriInfos[uri]; ok {
			uriInfos[uri] = exisiting
			continue
		}
		uriInfos[uri] = uriInfo{
			recentFailures: NewCourseExponentialDecayReservoir(s.nanoClock, failureMemory),
		}
	}

	s.uriInfos = uriInfos
	return
}

func (s *balancedSelector) next() (string, error) {
	if len(s.uriInfos) == 0 {
		return "", werror.Error("no valid connections available")
	}
	uris := make([]string, 0, len(s.uriInfos))
	scores := make(map[string]int32, len(s.uriInfos))
	for uri, info := range s.uriInfos {
		uris = append(uris, uri)
		scores[uri] = info.computeScore()
	}
	// Pre-shuffle to avoid overloading first URI when no request are in-flight
	rand.Shuffle(len(uris), func(i, j int) {
		uris[i], uris[j] = uris[j], uris[i]
	})
	sort.Slice(uris, func(i, j int) bool {
		return scores[uris[i]] < scores[uris[j]]
	})
	return uris[0], nil
}

func (s *balancedSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	baseURI := getBaseURI(req.URL)
	s.updateInflight(baseURI, 1)
	defer s.updateInflight(baseURI, -1)

	resp, err := next.RoundTrip(req)
	if resp == nil || err != nil {
		s.updateRecentFailures(baseURI, failureWeight)
		return nil, err
	}
	statusCode := resp.StatusCode
	if isGlobalQosStatus(statusCode) || isServerErrorRange(statusCode) {
		s.updateRecentFailures(baseURI, failureWeight)
	} else if isClientError(statusCode) {
		s.updateRecentFailures(baseURI, failureWeight/100)
	}
	return resp, nil
}

func (s *balancedSelector) updateInflight(uri string, score int32) {
	s.Lock()
	defer s.Unlock()

	info, ok := s.uriInfos[uri]
	if ok {
		atomic.AddInt32(&info.inflight, score)
	}
}

func (s *balancedSelector) updateRecentFailures(uri string, weight float64) {
	s.Lock()
	defer s.Unlock()

	info, ok := s.uriInfos[uri]
	if ok {
		info.recentFailures.Update(weight)
	}
}

type uriInfo struct {
	inflight       int32
	recentFailures CourseExponentialDecayReservoir
}

func (i *uriInfo) computeScore() int32 {
	return atomic.LoadInt32(&i.inflight) + int32(math.Round(i.recentFailures.Get()))
}

func getBaseURI(u *url.URL) string {
	uCopy := url.URL{
		Scheme: u.Scheme,
		Opaque: u.Opaque,
		User:   u.User,
		Host:   u.Host,
	}
	return uCopy.String()
}

func isGlobalQosStatus(statusCode int) bool {
	return statusCode == StatusCodeRetryOther || statusCode == StatusCodeUnavailable
}

func isServerErrorRange(statusCode int) bool {
	return statusCode/100 == 5
}

func isClientError(statusCode int) bool {
	return statusCode/100 == 4
}

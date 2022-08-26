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
	"net/http"
	"sync"
	"time"
)

const (
	// defaultResurrectDuration is the amount of time after which
	// we resurrect failed URIs
	defaultResurrectDuration = time.Second * 60
	meshSchemePrefix         = "mesh-"
)

type serverScorer struct {
	sync.RWMutex

	failedURIs map[string]struct{}
}

// NewServerSelector returns a URISelector that keeps returns URIs for server which are not returning server side
// errors. It is used as middleware to track server side request failures to future requests from hitting known bad
// servers.
func NewServerSelector() URISelector {
	return &serverScorer{failedURIs: make(map[string]struct{})}
}

// select implements URISelector
func (s *serverScorer) Select(uris []string, _ http.Header) ([]string, error) {
	s.RLock()
	defer s.RUnlock()

	availableURIs := make([]string, 0, len(uris))
	for _, uri := range uris {
		if _, ok := s.failedURIs[uri]; ok {
			continue
		}
		availableURIs = append(availableURIs, uri)
	}
	// if all connections are "failed", then return them all
	if len(availableURIs) == 0 {
		return uris, nil
	}
	return availableURIs, nil
}

// RoundTrip implements URISelector
func (s *serverScorer) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, err := next.RoundTrip(req)
	errCode, ok := StatusCodeFromError(err)
	// fall back to the status code from the response
	if !ok && resp != nil {
		errCode = resp.StatusCode
	}

	if isThrottle, ressurectAfter := isThrottleResponse(resp, errCode); isThrottle {
		s.markBackoffURI(req, ressurectAfter)
	} else if isUnavailableResponse(resp, errCode) {
		// 503: go to next node
		s.markBackoffURI(req, defaultResurrectDuration)
	} else if resp == nil {
		// if we get a nil response, we can assume there is a problem with host and can move on to the next.
		s.markBackoffURI(req, defaultResurrectDuration)
	}

	return resp, err
}

func (s *serverScorer) markBackoffURI(req *http.Request, dur time.Duration) {
	// if duration is equal to zero, then use defaultResurrectDuration
	if dur == 0 {
		dur = defaultResurrectDuration
	}
	reqURL := getBaseURI(req.URL)
	s.Lock()
	defer s.Unlock()

	s.failedURIs[reqURL] = struct{}{}

	time.AfterFunc(dur, func() {
		s.Lock()
		defer s.Unlock()
		delete(s.failedURIs, reqURL)
	})
}

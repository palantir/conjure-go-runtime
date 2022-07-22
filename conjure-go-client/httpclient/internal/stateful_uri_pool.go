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
	"net/url"
	"sync"
	"time"

	"github.com/palantir/pkg/refreshable"
)

const (
	// defaultResurrectDuration is the amount of time after which
	// we resurrect failed URIs
	defaultResurrectDuration = time.Second * 60
	meshSchemePrefix         = "mesh-"
)

type statefulURIPool struct {
	sync.RWMutex

	uris       []string
	failedURIs map[string]struct{}
}

// NewStatefulURIPool returns a URIPool that keeps track of a
// refeshable set of possible URIs. It can be used as middleware to track
// server side request failures to future requests from hitting known bad servers.
func NewStatefulURIPool(uris refreshable.StringSlice) URIPool {
	s := &statefulURIPool{}
	s.updateURIs(uris.CurrentStringSlice())

	_ = uris.SubscribeToStringSlice(s.updateURIs)
	return s
}

// NumURIs implements URIPool
func (s *statefulURIPool) NumURIs() int {
	s.RLock()
	defer s.RUnlock()

	return len(s.uris)
}

// URIs implements URIPool
func (s *statefulURIPool) URIs() []string {
	s.RLock()
	defer s.RUnlock()

	uris := make([]string, 0, len(s.uris))
	for _, uri := range s.uris {
		if _, ok := s.failedURIs[uri]; ok {
			continue
		}
		uris = append(uris, uri)
	}
	// if all connections are "failed", then return them all
	if len(uris) == 0 {
		return s.uris
	}
	return uris
}

// RoundTrip implements URIPool
func (s *statefulURIPool) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	resp, respErr := next.RoundTrip(req)
	errCode, _ := StatusCodeFromError(respErr)

	if isThrottle, ressurectAfter := isThrottleResponse(resp, errCode); isThrottle {
		s.markBackoffURI(req, ressurectAfter)
	}
	if isUnavailableResponse(resp, errCode) {
		// 503: go to next node
		s.markBackoffURI(req, defaultResurrectDuration)
	}
	// TODO(dtrejo): Do we need to handle redirects or does the underlying http.Client do that for us?
	//if isTryOther, isPermenant, otherURI := isRetryOtherResponse(resp, respErr, errCode); isTryOther {
	//	s.markBackoffURI(req, defaultResurrectDuration)
	//}
	if errCode >= http.StatusBadRequest && errCode < http.StatusInternalServerError {
		// nothing to do
	}
	if resp == nil {
		// if we get a nil response, we can assume there is a problem with host and can move on to the next.
		s.markBackoffURI(req, defaultResurrectDuration)
	}

	return resp, respErr
}

func (s *statefulURIPool) updateURIs(uris []string) {
	result := make([]string, 0, len(uris))
	for _, uri := range uris {
		// validate URIs by parsing them
		u, err := url.Parse(uri)
		if err != nil {
			// ignore invalid uris
			continue
		}
		result = append(result, getBaseURI(u))
	}

	s.Lock()
	defer s.Unlock()
	s.uris = result
	s.failedURIs = make(map[string]struct{}, len(uris))
}

func (s *statefulURIPool) markBackoffURI(req *http.Request, dur time.Duration) {
	// if duration is equal to zero, then don't mark the URI as failed
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

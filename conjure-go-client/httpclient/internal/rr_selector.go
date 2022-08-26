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
	"math/rand"
	"net/http"
	"sync"
)

type roundRobinSelector struct {
	sync.Mutex
	source rand.Source

	prevURIs []string
	offset   int
}

// NewRoundRobinURISelector returns a URI scorer that uses a round robin algorithm for selecting URIs when scoring
// using a rand.Rand seeded by the nanoClock function. The middleware no-ops on each request. This selector will always
// return one URI.
func NewRoundRobinURISelector(nanoClock func() int64) URISelector {
	return &roundRobinSelector{
		source:   rand.NewSource(nanoClock()),
		prevURIs: []string{},
	}
}

// Select implements Selector interface
func (s *roundRobinSelector) Select(uris []string, _ http.Header) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	s.updateURIs(uris)
	s.offset = (s.offset + 1) % len(uris)
	return []string{uris[s.offset]}, nil
}

// updateURIs determines whether we need to update the stored prevURIs because the current set of URIs differ from the
// last observed URIs. When the URIs we randomize to get a new offest.
func (s *roundRobinSelector) updateURIs(uris []string) {
	reset := false
	if len(s.prevURIs) == 0 {
		reset = true
	}
	for i, uri := range s.prevURIs {
		if uri != uris[i] {
			reset = true
			break
		}
	}

	if reset {
		s.prevURIs = uris
		// randomize offset on reinit
		s.offset = rand.New(s.source).Intn(len(uris))
	}
}

func (s *roundRobinSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

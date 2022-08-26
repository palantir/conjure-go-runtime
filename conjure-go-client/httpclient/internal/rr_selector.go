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
)

type roundRobinSelector struct {
	sync.Mutex
	nanoClock func() int64

	offset int
}

// NewRoundRobinURISelector returns a URI scorer that uses a round robin algorithm for selecting URIs when scoring
// using a rand.Rand seeded by the nanoClock function. The middleware no-ops on each request.
func NewRoundRobinURISelector(nanoClock func() int64) URISelector {
	return &roundRobinSelector{
		nanoClock: nanoClock,
	}
}

// Select implements Selector interface
func (s *roundRobinSelector) Select(uris []string, _ http.Header) ([]string, error) {
	s.Lock()
	defer s.Unlock()

	s.offset = (s.offset + 1) % len(uris)

	return []string{uris[s.offset]}, nil
}

func (s *roundRobinSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

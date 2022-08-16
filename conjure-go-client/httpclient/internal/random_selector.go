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
	"math/rand"
	"net/http"
	"sync"

	werror "github.com/palantir/witchcraft-go-error"
)

type randomSelector struct {
	sync.Mutex
	nanoClock func() int64
}

// NewRandomURISelector returns a URI scorer that randomizes the order of URIs when scoring using a rand.Rand
// seeded by the nanoClock function. The middleware no-ops on each request.
func NewRandomURISelector(nanoClock func() int64) URISelector {
	return &randomSelector{
		nanoClock: nanoClock,
	}
}

// Select implements estransport.Selector interface
func (s *randomSelector) Select(uris []string, _ http.Header) (string, error) {
	s.Lock()
	defer s.Unlock()

	return s.next(uris)
}

func (s *randomSelector) next(uris []string) (string, error) {
	if len(uris) == 0 {
		return "", werror.Error("no valid connections available")
	}
	rand.New(rand.NewSource(s.nanoClock())).Shuffle(len(uris), func(i, j int) {
		uris[i], uris[j] = uris[j], uris[i]
	})

	return uris[0], nil
}

func (s *randomSelector) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

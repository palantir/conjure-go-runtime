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
	"hash/fnv"
	"net/http"
	"sort"
)

type rendezvousHashScorer struct {
	uris          []string
	hashHeaderKey string
	nanoClock     func() int64
}

func (r *rendezvousHashScorer) GetURIsInOrderOfIncreasingScore(header http.Header) []string {
	hashHeaderValues, ok := header[r.hashHeaderKey]
	if !ok || len(hashHeaderValues) == 0 {
		return getURIsInRandomOrder(r.uris, r.nanoClock())
	}
	fnv.New64a()
	uris := make([]string, 0, len(r.uris))
	scores := make(map[string]uint32, len(r.uris))
	hash := fnv.New32()
	for _, uri := range r.uris {
		hash.Reset()
		if _, err := hash.Write([]byte(uri)); err != nil {
			return nil
		}
		for _, value := range hashHeaderValues {
			if _, err := hash.Write([]byte(value)); err != nil {
				return nil
			}
		}
		uris = append(uris, uri)
		scores[uri] = hash.Sum32()
	}
	sort.Slice(uris, func(i, j int) bool {
		return scores[uris[i]] < scores[uris[j]]
	})
	return uris
}

func (r *rendezvousHashScorer) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	return next.RoundTrip(req)
}

// NewRendezvousHashScoringMiddleware returns a URI scorer that generates a deterministic ordering of the URIs
// based on the value of a header. The scorer hashes the header value along with the URI and sorts the URIs based on
// the value of the hash.
//
// The intent of this scoring strategy is to provide server-side read and write locality for clients - by providing the
// configured header based on a key from content in the request, clients can expect that requests for the same key are
// generally routed to the same URI. It is important clients do not rely on always reaching the same URIs for
// correctness as requests will be retried with other URIs in the case of failures.
//
// When the header is not present, the scorer randomizes the order of URIs by using a rand.Rand
// seeded by the nanoClock function.
//
// The middleware no-ops on each request.
func NewRendezvousHashScoringMiddleware(uris []string, hashHeader string, nanoClock func() int64) URIScoringMiddleware {
	return &rendezvousHashScorer{
		uris:          uris,
		hashHeaderKey: hashHeader,
		nanoClock:     nanoClock,
	}
}

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
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const (
	hashHeader = "X-Route-Hash"
)

func TestRendezvousHashScorerRandomizesWithoutHeader(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewRendezvousHashURIScoringMiddleware(uris, hashHeader, func() int64 { return time.Now().UnixNano() })
	scoredUris1 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{})
	scoredUris2 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{})
	assert.ElementsMatch(t, scoredUris1, scoredUris2)
	assert.NotEqual(t, scoredUris1, scoredUris2)
}

func TestRendezvousHashScorerSortsUrisDeterministically(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewRendezvousHashURIScoringMiddleware(uris, hashHeader, func() int64 { return time.Now().UnixNano() })
	scoredUris1 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{hashHeader: []string{"foo"}})
	scoredUris2 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{hashHeader: []string{"foo"}})
	assert.Equal(t, scoredUris1, scoredUris2)
}

func TestRendezvousHashScorerMultipleHashHeaders(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewRendezvousHashURIScoringMiddleware(uris, hashHeader, func() int64 { return time.Now().UnixNano() })
	scoredUris1 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{hashHeader: []string{"hash1", "hash2"}})
	scoredUris2 := scorer.GetURIsInOrderOfIncreasingScore(http.Header{hashHeader: []string{"hash1", "hash2", "hash3"}})
	assert.ElementsMatch(t, scoredUris1, scoredUris2)
	assert.NotEqual(t, scoredUris1, scoredUris2)
}
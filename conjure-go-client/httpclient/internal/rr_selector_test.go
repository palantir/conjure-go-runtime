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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRoundRobinSelector_Select(t *testing.T) {
	uris := []string{"uri1", "uri2", "uri3", "uri4", "uri5"}
	scorer := NewRoundRobinURISelector(func() int64 { return time.Now().UnixNano() })

	const iterations = 100
	observed := make(map[string]int, iterations)
	for i := 0; i < iterations; i++ {
		uri, err := scorer.Select(uris, nil)
		assert.NoError(t, err)
		observed[uri] = observed[uri] + 1
	}

	occurences := make([]int, 0, len(observed))
	for _, count := range observed {
		occurences = append(occurences, count)
	}

	for _, v := range occurences {
		assert.Equal(t, occurences[0], v)
	}
}

// Copyright (c) 2019 Palantir Technologies. All rights reserved.
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

package httpclient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJoinURIandPath(t *testing.T) {
	for _, test := range []struct {
		baseURI  string
		reqPath  string
		expected string
	}{
		{
			"https://localhost",
			"/api",
			"https://localhost/api",
		},
		{
			"https://localhost:443",
			"/api",
			"https://localhost:443/api",
		},
		{
			"https://localhost:443",
			"api",
			"https://localhost:443/api",
		},
		{
			"https://localhost:443/",
			"api",
			"https://localhost:443/api",
		},
		{
			"https://localhost:443/foo/",
			"/api",
			"https://localhost:443/foo/api",
		},
		{
			"https://localhost:443/foo//////",
			"////api/",
			"https://localhost:443/foo/api/",
		},
	} {
		t.Run("", func(t *testing.T) {
			actual, err := joinURIAndPath(test.baseURI, test.reqPath)
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

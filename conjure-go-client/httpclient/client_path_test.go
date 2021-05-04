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
		baseURI     string
		reqPath     string
		baseURIOnly bool
		expected    string
	}{
		{
			"https://localhost",
			"/api",
			false,
			"https://localhost/api",
		},
		{
			"https://localhost:443",
			"/api",
			false,
			"https://localhost:443/api",
		},
		{
			"https://localhost:443",
			"api",
			false,
			"https://localhost:443/api",
		},
		{
			"https://localhost:443/",
			"api",
			false,
			"https://localhost:443/api",
		},
		{
			"https://localhost:443/foo/",
			"/api",
			false,
			"https://localhost:443/foo/api",
		},
		{
			"https://localhost:443/foo//////",
			"////api/",
			false,
			"https://localhost:443/foo/api/",
		},
		{
			"https://localhost:443/foo/",
			"/api",
			false,
			"https://localhost:443/foo/api",
		},
		{
			"https://localhost",
			"",
			false,
			"https://localhost",
		},
		{
			"https://localhost/api",
			"",
			false,
			"https://localhost/api",
		},
		{
			"https://localhost/api",
			"/another/api/path",
			true,
			"https://localhost/api",
		},
		{
			"https://localhost/api",
			"",
			true,
			"https://localhost/api",
		},
	} {
		t.Run("", func(t *testing.T) {
			actual, err := joinURIAndPath(test.baseURI, test.reqPath, test.baseURIOnly)
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

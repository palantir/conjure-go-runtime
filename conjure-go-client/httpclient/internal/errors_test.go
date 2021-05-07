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
	"testing"

	werror "github.com/palantir/witchcraft-go-error"
	"github.com/stretchr/testify/assert"
)

func TestGetStatusCodeFromError(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		err                   error
		expectStatusCodeExist bool
		expectStatusCode      int
	}{
		{
			name:                  "no status code",
			err:                   werror.Error("200"),
			expectStatusCodeExist: false,
			expectStatusCode:      0,
		},
		{
			name: "status code 200",
			err: werror.Error("200",
				werror.SafeParam("statusCode", 200)),
			expectStatusCodeExist: true,
			expectStatusCode:      200,
		},
		{
			name: "status code non int",
			err: werror.Error("200",
				werror.SafeParam("statusCode", "200")),
			expectStatusCodeExist: false,
			expectStatusCode:      0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, exist := StatusCodeFromError(tc.err)
			assert.Equal(t, tc.expectStatusCodeExist, exist)
			assert.Equal(t, tc.expectStatusCode, code)
		})
	}
}

func TestGetLocationFromError(t *testing.T) {
	for _, tc := range []struct {
		name                string
		err                 error
		expectLocationExist bool
		expectLocation      string
	}{
		{
			name: "200 no location",
			err: werror.Error("200",
				werror.SafeParam("statusCode", 200)),
			expectLocationExist: false,
			expectLocation:      "",
		},
		{
			name: "307 with location",
			err: werror.Error("307",
				werror.SafeParam("statusCode", 307),
				werror.UnsafeParam("location", "https://google.com")),
			expectLocationExist: true,
			expectLocation:      "https://google.com",
		},
		{
			name: "307 without location",
			err: werror.Error("307",
				werror.SafeParam("statusCode", 307),
				werror.UnsafeParam("location", "")),
			expectLocationExist: true,
			expectLocation:      "",
		},
		{
			name: "307 with non string location",
			err: werror.Error("307",
				werror.SafeParam("statusCode", 307),
				werror.UnsafeParam("location", 12345)),
			expectLocationExist: false,
			expectLocation:      "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			location, exist := LocationFromError(tc.err)
			assert.Equal(t, tc.expectLocationExist, exist)
			assert.Equal(t, tc.expectLocation, location)
		})
	}
}

// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package useragent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProduct(t *testing.T) {
	for _, test := range []struct {
		Name        string
		Product     Product
		ExpectedErr string
	}{
		{
			Name:        "empty",
			ExpectedErr: "product name is not valid for User-Agent",
		},
		{
			Name: "empty version",
			Product: Product{
				name: "test",
			},
			ExpectedErr: "product version is not valid for User-Agent",
		},
		{
			Name: "ok product",
			Product: Product{
				name:    "foo",
				version: "1.0.0",
			},
		},
		{
			Name: "ok product with comments",
			Product: Product{
				name:     "foo",
				version:  "1.0.0",
				comments: []string{"comment one", "comment two"},
			},
		},
		{
			Name: "invalid name",
			Product: Product{
				name:     ";;",
				version:  "1.0.0",
				comments: []string{"comment one", "comment two"},
			},
			ExpectedErr: "product name is not valid for User-Agent",
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			p, err := NewProduct(test.Product.name, test.Product.version, test.Product.comments...)
			if test.ExpectedErr == "" {
				require.NoError(t, err)
				assert.Equal(t, test.Product, p)
			} else {
				require.EqualError(t, err, test.ExpectedErr)
			}
		})
	}
}

func TestBuilder_String(t *testing.T) {
	for _, test := range []struct {
		Name        string
		In          []Product
		Expected    string
		ExpectedErr string
	}{
		{
			Name:     "empty",
			In:       []Product{},
			Expected: "",
		},
		{
			Name: "single product",
			In: []Product{
				{
					name:    "foo",
					version: "1.0.0",
				},
			},
			Expected: "foo/1.0.0",
		},
		{
			Name: "single product with comments",
			In: []Product{
				{
					name:     "foo",
					version:  "1.0.0",
					comments: []string{"comment one", "comment two"},
				},
			},
			Expected: "foo/1.0.0 (comment one, comment two)",
		},
		{
			Name: "two products with comments",
			In: []Product{
				{
					name:     "foo",
					version:  "1.0.0",
					comments: []string{"comment one"},
				},
				{
					name:     "bar",
					version:  "2.0.0",
					comments: []string{"comment two"},
				},
			},
			Expected: "bar/2.0.0 (comment two) foo/1.0.0 (comment one)",
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			stack := Builder{}
			stack.Push(test.In...)
			result := stack.String()
			assert.Equal(t, test.Expected, result)
		})
	}
}

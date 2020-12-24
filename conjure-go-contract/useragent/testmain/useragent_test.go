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

package main

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/useragent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultUserAgent(t *testing.T) {
	testProduct, err := useragent.NewProduct("test", "v1.2.3", "comment1", "comment2")
	require.NoError(t, err)
	useragent.Default.Push(testProduct)

	expected := fmt.Sprintf(`test/v1.2.3 \(comment1, comment2\) conjure-go-runtime/v2\.2\.0 golang/1\.\d+\.\d+ \(%s/%s\)`, runtime.GOOS, runtime.GOARCH)
	actual := useragent.Default.String()
	assert.Regexp(t, expected, actual)
}

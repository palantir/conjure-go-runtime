// Copyright (c) 2024 Palantir Technologies. All rights reserved.
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
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentLengthInMemory(t *testing.T) {
	t.Run("bytes.Buffer", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewBuffer([]byte("hello"))))
	})
	t.Run("bytes.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(bytes.NewReader([]byte("hello"))))
	})
	t.Run("strings.Reader", func(t *testing.T) {
		assert.EqualValues(t, 5, contentLengthInMemory(strings.NewReader("hello")))
	})
}

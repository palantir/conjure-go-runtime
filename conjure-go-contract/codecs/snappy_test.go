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

package codecs

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnappyCompression(t *testing.T) {
	// create a compressible string
	input := strings.Join([]string{
		strings.Repeat("a", 10),
		strings.Repeat("b", 10),
		strings.Repeat("a", 10),
		strings.Repeat("c", 10),
		strings.Repeat("a", 10),
	}, "")
	snappyEncoder := Snappy(Plain)

	t.Run("Encode/Decode", func(t *testing.T) {
		var buf bytes.Buffer
		err := snappyEncoder.Encode(&buf, input)
		require.NoError(t, err)

		// assert encoded message compressed
		encoded := buf.String()
		assert.Less(t, len(encoded), len(input))

		var actual string
		err = snappyEncoder.Decode(strings.NewReader(encoded), &actual)
		require.NoError(t, err)

		require.Equal(t, input, actual)
	})
	t.Run("Marshal/Unmarshal", func(t *testing.T) {
		encoded, err := snappyEncoder.Marshal(input)
		require.NoError(t, err)

		// assert encoded message compressed
		assert.Less(t, len(encoded), len(input))

		var actual string
		err = snappyEncoder.Unmarshal(encoded, &actual)
		require.NoError(t, err)

		require.Equal(t, input, actual)
	})
}

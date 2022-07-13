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

	"github.com/stretchr/testify/require"
)

func TestSnappyFramedCompression(t *testing.T) {
	input := "hello world"
	snappyEncoder := SnappyFramed(Plain)

	var buf bytes.Buffer
	err := snappyEncoder.Encode(&buf, input)
	require.NoError(t, err)

	var actual string
	err = snappyEncoder.Decode(strings.NewReader(buf.String()), &actual)
	require.NoError(t, err)

	require.Equal(t, input, actual)
}

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

package codecs_test

import (
	"bytes"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/stretchr/testify/require"
)

func TestCodecBinary(t *testing.T) {
	data := []byte(`1234567890`)

	t.Run("Unmarshal", func(t *testing.T) {
		buf := bytes.Buffer{}
		err := codecs.Binary.Unmarshal(data, &buf)
		require.NoError(t, err)
		require.Equal(t, data, buf.Bytes())
	})
	t.Run("Marshal", func(t *testing.T) {
		out, err := codecs.Binary.Marshal(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, data, out)
	})
}

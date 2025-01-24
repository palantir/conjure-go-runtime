// Copyright (c) 2025 Palantir Technologies. All rights reserved.
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

func TestCodecYAML(t *testing.T) {
	type TestStruct struct {
		Key   string `yaml:"key"`
		Value string `yaml:"value"`
	}
	msg := &TestStruct{
		Key:   "key",
		Value: "value",
	}

	yamlCodec := codecs.YAML()

	t.Run("Marshal/Unmarshal", func(t *testing.T) {
		out, err := yamlCodec.Marshal(msg)
		require.NoError(t, err)

		actual := &TestStruct{}
		err = yamlCodec.Unmarshal(out, actual)
		require.NoError(t, err)
		require.Equal(t, msg, actual)
	})
	t.Run("Encode/Decode", func(t *testing.T) {
		var buf bytes.Buffer
		err := yamlCodec.Encode(&buf, msg)
		require.NoError(t, err)

		actual := &TestStruct{}
		err = yamlCodec.Decode(bytes.NewReader(buf.Bytes()), actual)
		require.NoError(t, err)
		require.Equal(t, msg, actual)
	})
}

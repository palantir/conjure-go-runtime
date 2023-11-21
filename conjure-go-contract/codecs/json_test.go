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

package codecs_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/stretchr/testify/require"
)

func TestJSON_UsesMethodsWhenImplemented(t *testing.T) {
	t.Run("Marshal_Unmarshal", func(t *testing.T) {
		obj := map[string]testJSONObject{}
		err := codecs.JSON.Unmarshal([]byte(`{"key":null}`), &obj)
		require.NoError(t, err)
		require.Contains(t, obj, "key")
		require.Equal(t, `null`, string(obj["key"].data))
		data, err := codecs.JSON.Marshal(obj)
		require.NoError(t, err)
		require.Equal(t, `{"key":null}`, string(data))
	})
	t.Run("Encode_Decode", func(t *testing.T) {
		obj := map[string]testJSONObject{}
		r := strings.NewReader(`{"key":"abc"}`)
		err := codecs.JSON.Decode(r, &obj)
		require.NoError(t, err)
		require.Contains(t, obj, "key")
		require.Equal(t, `"abc"`, string(obj["key"].data))
		out := bytes.Buffer{}
		err = codecs.JSON.Encode(&out, obj)
		require.NoError(t, err)
		require.Equal(t, "{\"key\":\"abc\"}\n", out.String())
	})
}

type testJSONObject struct {
	data []byte
}

func (t testJSONObject) MarshalJSON() ([]byte, error) {
	return t.data, nil
}

func (t *testJSONObject) UnmarshalJSON(data []byte) error {
	t.data = data
	return nil
}

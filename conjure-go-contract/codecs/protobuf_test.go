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
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/test"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/stretchr/testify/require"
)

func TestCodecProtobuf_Serde(t *testing.T) {
	msg := &test.NinOptNative{
		Field15: []byte(`1234567890`),
	}

	out, err := codecs.Protobuf.Marshal(msg)
	require.NoError(t, err)

	actual := &test.NinOptNative{}
	err = codecs.Protobuf.Unmarshal(out, actual)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, actual))
}

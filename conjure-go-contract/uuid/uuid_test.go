// Copyright (c) 2018 Palantir Technologies. All rights reserved.
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

package uuid_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/conjure-go-contract/uuid"
)

var testUUID = uuid.UUID{
	0x0, 0x1, 0x2, 0x3,
	0x4, 0x5, 0x6, 0x7,
	0x8, 0x9, 0xA, 0xB,
	0xC, 0xD, 0xE, 0xF,
}

func TestUUID_MarshalJSON(t *testing.T) {
	marshalledUUID, err := codecs.JSON.Marshal(testUUID)
	assert.NoError(t, err)
	assert.Equal(t, `"00010203-0405-0607-0809-0a0b0c0d0e0f"`, string(marshalledUUID))
}

func TestUUID_UnmarshalJSON(t *testing.T) {
	t.Run("correct lower case", func(t *testing.T) {
		var actual uuid.UUID
		err := codecs.JSON.Unmarshal([]byte(`"00010203-0405-0607-0809-0a0b0c0d0e0f"`), &actual)
		assert.NoError(t, err)
		assert.Equal(t, testUUID, actual)
	})

	t.Run("correct upper case", func(t *testing.T) {
		var actual uuid.UUID
		err := codecs.JSON.Unmarshal([]byte(`"00010203-0405-0607-0809-0A0B0C0D0E0F"`), &actual)
		assert.NoError(t, err)
		assert.Equal(t, testUUID, actual)
	})

	t.Run("incorrect group", func(t *testing.T) {
		var actual uuid.UUID
		err := codecs.JSON.Unmarshal([]byte(`"00010203-04Z5-0607-0809-0A0B0C0D0E0F"`), &actual)
		assert.EqualError(t, err, "failed to decode JSON-encoded value: invalid UUID format")
	})
}

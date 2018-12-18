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

package errors_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/palantir/conjure-go/conjure/types/conjuretype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/conjure-go-contract/errors"
)

var testSerializableError = errors.SerializableError{
	ErrorCode:       errors.Timeout,
	ErrorName:       "MyApplication:Timeout",
	ErrorInstanceID: conjuretype.NewUUID(),
	Parameters: json.RawMessage(`{
    "metadata": {
      "keyB": 4
    }
  }`),
}

var testErrorJSON = fmt.Sprintf(`{
  "errorCode": "TIMEOUT",
  "errorName": "MyApplication:Timeout",
  "errorInstanceId": "%s",
  "parameters": {
    "metadata": {
      "keyB": 4
    }
  }
}`, testSerializableError.ErrorInstanceID)

func TestSerializableError_MarshalJSON(t *testing.T) {
	marshalledError, err := codecs.JSON.Marshal(testSerializableError)
	assert.NoError(t, err)

	var buffer bytes.Buffer
	require.NoError(t, json.Indent(&buffer, marshalledError, "", "  "))
	assert.Equal(t, testErrorJSON, buffer.String())
}

func TestSerializableError_UnmarshalJSON(t *testing.T) {
	var unmarshalled errors.SerializableError
	err := codecs.JSON.Unmarshal([]byte(testErrorJSON), &unmarshalled)
	assert.NoError(t, err)
	assert.Equal(t, testSerializableError.ErrorCode, unmarshalled.ErrorCode)
	assert.Equal(t, testSerializableError.ErrorName, unmarshalled.ErrorName)
	assert.Equal(t, testSerializableError.ErrorInstanceID, unmarshalled.ErrorInstanceID)
	assert.Equal(t, testSerializableError.Parameters, json.RawMessage(`{
    "metadata": {
      "keyB": 4
    }
  }`))
}

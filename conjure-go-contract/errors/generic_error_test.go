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

// Copyright 2016 Palantir Technologies. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package errors

import (
	"fmt"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	err := NewError(
		MustErrorType(Timeout, "MyApplication:DatabaseTimeout"),
		wparams.NewSafeParamStorer(map[string]interface{}{"ttl": "10s"}),
	)
	assert.EqualError(t, err, fmt.Sprintf("TIMEOUT MyApplication:DatabaseTimeout (%s)", err.InstanceID()))
}

func TestError_CodecsJSONEscapesHTML(t *testing.T) {
	e := NewError(
		MustErrorType(Timeout, "MyApplication:Timeout"),
		wparams.NewSafeParamStorer(map[string]interface{}{"htmlKey": "something&something"}),
	)

	marshaledError, err := codecs.JSON.Marshal(e)
	assert.NoError(t, err)
	assert.Regexp(t, `something&something`, string(marshaledError))
}

func TestError_NewError_Then_MarshalJSON_Then_UnmarshalJSON_And_Unpack(t *testing.T) {
	e := NewError(
		MustErrorType(Timeout, "MyApplication:Timeout"),
		wparams.NewSafeAndUnsafeParamStorer(
			map[string]interface{}{"safeKey": "safeValue"},
			map[string]interface{}{"unsafeKey": "unsafeValue"},
		),
	)
	expectedJSON := fmt.Sprintf(`{
  "errorCode": "TIMEOUT",
  "errorInstanceId": "%s",
  "errorName": "MyApplication:Timeout",
  "parameters": {
    "safeKey": "safeValue",
    "unsafeKey": "unsafeValue"
  }
}`, e.InstanceID().String())

	marshaledError, err := codecs.JSON.Marshal(e)
	require.NoError(t, err)
	require.JSONEq(t, expectedJSON, string(marshaledError))

	unmarshaledError, err := UnmarshalError(marshaledError)
	require.NoError(t, err)

	assert.EqualError(t, unmarshaledError, e.Error())
	assert.Equal(t, e.Name(), unmarshaledError.Name())
	assert.Equal(t, e.Code(), unmarshaledError.Code())
	assert.Equal(t, e.InstanceID(), unmarshaledError.InstanceID())
	assert.Equal(t, mergeParams(e), mergeParams(unmarshaledError))
}

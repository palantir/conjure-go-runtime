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
	"io/ioutil"
	"net/http/httptest"
	"testing"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/errors"
	wparams "github.com/palantir/witchcraft-go-params"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteErrorResponse_ValidateJSON(t *testing.T) {
	testError := errors.NewError(errors.MustErrorType(errors.Timeout, "MyApplication:Timeout"),
		wparams.NewSafeParamStorer(map[string]interface{}{
			"metadata": struct {
				KeyB int `json:"keyB"`
			}{
				KeyB: 4,
			},
		}))

	testErrorJSON := fmt.Sprintf(`{
  "errorCode": "TIMEOUT",
  "errorName": "MyApplication:Timeout",
  "errorInstanceId": "%s",
  "parameters": {
    "metadata": {
      "keyB": 4
    }
  }
}`, testError.InstanceID())

	recorder := httptest.NewRecorder()
	errors.WriteErrorResponse(recorder, testError)
	response := recorder.Result()

	assert.Equal(t, "application/json; charset=utf-8", response.Header.Get("Content-Type"))
	body, err := ioutil.ReadAll(response.Body)
	require.NoError(t, err)

	var buffer bytes.Buffer
	require.NoError(t, json.Indent(&buffer, body, "", "  "))
	assert.Equal(t, testErrorJSON, buffer.String())
}

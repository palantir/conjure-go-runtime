// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	"github.com/palantir/pkg/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalError(t *testing.T) {
	errors.RegisterErrorType(testErrorName, reflect.TypeOf(testErrorType{}))
	for _, test := range []struct {
		name      string
		in        errors.SerializableError
		inRaw     string
		expectErr string
		verify    func(t *testing.T, actual errors.Error)
	}{
		{
			name: "default timeout",
			in: errors.SerializableError{
				ErrorCode:       errors.DefaultTimeout.Code(),
				ErrorName:       errors.DefaultTimeout.Name(),
				ErrorInstanceID: uuid.NewUUID(),
				Parameters:      json.RawMessage(`{"ttl":"10s"}`),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{"ttl": "10s"}, actual.UnsafeParams())
			},
		},
		{
			name: "custom timeout",
			in: errors.SerializableError{
				ErrorCode:       errors.DefaultTimeout.Code(),
				ErrorName:       "MyApplication:Timeout",
				ErrorInstanceID: uuid.NewUUID(),
				Parameters:      json.RawMessage(`{"ttl":"10s"}`),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{"ttl": "10s"}, actual.UnsafeParams())
			},
		},
		{
			name: "custom not found",
			in: errors.SerializableError{
				ErrorCode:       errors.DefaultTimeout.Code(),
				ErrorName:       "MyApplication:MissingData",
				ErrorInstanceID: uuid.NewUUID(),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{}, actual.UnsafeParams())
			},
		},
		{
			name: "custom client",
			in: errors.SerializableError{
				ErrorCode:       errors.CustomClient,
				ErrorName:       "MyApplication:CustomClientError",
				ErrorInstanceID: uuid.NewUUID(),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{}, actual.UnsafeParams())
			},
		},
		{
			name: "custom server",
			in: errors.SerializableError{
				ErrorCode:       errors.CustomServer,
				ErrorName:       "MyApplication:CustomServerError",
				ErrorInstanceID: uuid.NewUUID(),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{}, actual.UnsafeParams())
			},
		},
		{
			name: "registered error type",
			in: errors.SerializableError{
				ErrorCode:       errors.CustomClient,
				ErrorName:       testErrorName,
				ErrorInstanceID: uuid.NewUUID(),
				Parameters:      json.RawMessage(`{"intArg": 3, "stringArg": "foo"}`),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, testErrorTypeParams{IntArg: 3, StringArg: "foo"}, actual.(*testErrorType).Parameters)
				assert.Equal(t, map[string]interface{}{"intArg": 3, "errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{"stringArg": "foo"}, actual.UnsafeParams())
			},
		},
		{
			name: "unregistered error type",
			in: errors.SerializableError{
				ErrorCode:       errors.CustomClient,
				ErrorName:       "MyNamespace:MyOtherError",
				ErrorInstanceID: uuid.NewUUID(),
				Parameters:      json.RawMessage(`{"intArg": 3, "stringArg": "foo"}`),
			},
			verify: func(t *testing.T, actual errors.Error) {
				assert.Equal(t, map[string]interface{}{"errorInstanceId": actual.InstanceID(), "errorName": actual.Name()}, actual.SafeParams())
				assert.Equal(t, map[string]interface{}{"intArg": json.Number("3"), "stringArg": "foo"}, actual.UnsafeParams())
			},
		},
		{
			name:      "plaintext",
			inRaw:     "404 Not Found",
			expectErr: "failed to unmarshal body as conjure error: json.Unmarshal: json: cannot unmarshal number into Go value of type struct { Name string \"json:\\\"errorName\\\"\" }",
		},
		{
			name:      "other json",
			inRaw:     `{"foo":"bar"}`,
			expectErr: "failed to unmarshal body using registered type: json.Unmarshal: errors: error name does not match regexp `^(([A-Z][a-z0-9]+)+):(([A-Z][a-z0-9]+)+)$`",
		},
		{
			name:      "incomplete error json",
			inRaw:     `{"errorName":"Default:Internal"}`,
			expectErr: "failed to unmarshal body using registered type: json.Unmarshal: errors: invalid combination of default error name and error code",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var marshaledError []byte
			if test.inRaw == "" {
				var err error
				marshaledError, err = json.Marshal(test.in)
				require.NoError(t, err)
			} else {
				marshaledError = []byte(test.inRaw)
			}

			actual, err := errors.UnmarshalError(marshaledError)
			if test.expectErr == "" {
				require.NoError(t, err)
				assert.Equal(t, test.in.ErrorName, actual.Name())
				assert.Equal(t, test.in.ErrorCode, actual.Code())
				assert.Equal(t, test.in.ErrorInstanceID, actual.InstanceID())
				assert.Equal(t, test.in.ErrorInstanceID, actual.SafeParams()["errorInstanceId"])
				test.verify(t, actual)
			} else {
				assert.EqualError(t, err, test.expectErr)
			}
		})
	}
}

const testErrorName = "TestNamespace:TestError"

type testErrorType struct {
	ErrorCode       errors.ErrorCode    `json:"errorCode"`
	ErrorName       string              `json:"errorName"`
	ErrorInstanceID uuid.UUID           `json:"errorInstanceId"`
	Parameters      testErrorTypeParams `json:"parameters,omitempty"`
}

type testErrorTypeParams struct {
	IntArg    int    `json:"intArg,omitempty"`
	StringArg string `json:"stringArg,omitempty"`
}

func (e *testErrorType) Error() string {
	return fmt.Sprintf("%s (%s)", e.ErrorName, e.ErrorInstanceID)
}

func (e *testErrorType) Code() errors.ErrorCode {
	return e.ErrorCode
}

func (e *testErrorType) Name() string {
	return e.ErrorName
}

func (e *testErrorType) InstanceID() uuid.UUID {
	return e.ErrorInstanceID
}

func (e *testErrorType) SafeParams() map[string]interface{} {
	return map[string]interface{}{"intArg": e.Parameters.IntArg, "errorInstanceId": e.InstanceID(), "errorName": e.Name()}
}

func (e *testErrorType) UnsafeParams() map[string]interface{} {
	return map[string]interface{}{"stringArg": e.Parameters.StringArg}
}

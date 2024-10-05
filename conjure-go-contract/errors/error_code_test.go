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

package errors_test

import (
	"testing"

	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
	"github.com/stretchr/testify/assert"
)

func TestErrorCode_String(t *testing.T) {
	for ec, expectedString := range map[errors.ErrorCode]string{
		errors.Unauthorized:          "UNAUTHORIZED",
		errors.PermissionDenied:      "PERMISSION_DENIED",
		errors.InvalidArgument:       "INVALID_ARGUMENT",
		errors.NotFound:              "NOT_FOUND",
		errors.Conflict:              "CONFLICT",
		errors.RequestEntityTooLarge: "REQUEST_ENTITY_TOO_LARGE",
		errors.FailedPrecondition:    "FAILED_PRECONDITION",
		errors.Internal:              "INTERNAL",
		errors.Timeout:               "TIMEOUT",
		errors.CustomClient:          "CUSTOM_CLIENT",
		errors.CustomServer:          "CUSTOM_SERVER",
		errors.ServiceUnavailable:    "SERVICE_UNAVAILABLE",
		errors.ErrorCode(0):          "<invalid error code: 0>",
		errors.ErrorCode(200):        "<invalid error code: 200>",
	} {
		assert.Equal(t, expectedString, ec.String())
	}
}

var validErrorCodes = []errors.ErrorCode{
	errors.Unauthorized,
	errors.PermissionDenied,
	errors.InvalidArgument,
	errors.NotFound,
	errors.Conflict,
	errors.RequestEntityTooLarge,
	errors.FailedPrecondition,
	errors.Internal,
	errors.Timeout,
	errors.CustomClient,
	errors.CustomServer,
	errors.ServiceUnavailable,
}

func TestErrorCode_MarshalJSON(t *testing.T) {
	for _, ec := range validErrorCodes {
		t.Run(ec.String(), func(t *testing.T) {
			marshaledErrorCode, err := codecs.JSON.Marshal(ec)
			assert.NoError(t, err)
			assert.Equal(t, `"`+ec.String()+`"`, string(marshaledErrorCode))
		})
	}
}

func TestErrorCode_UnmarshalJSON(t *testing.T) {
	for _, ec := range validErrorCodes {
		t.Run(ec.String(), func(t *testing.T) {
			serialized := `"` + ec.String() + `"`
			var actual errors.ErrorCode
			err := codecs.JSON.Unmarshal([]byte(serialized), &actual)
			assert.NoError(t, err)
			assert.Equal(t, ec, actual)
		})
	}

	for _, s := range []string{
		"INVALID_ERROR_CODE",
	} {
		t.Run(s, func(t *testing.T) {
			serialized := `"` + s + `"`
			var actual errors.ErrorCode
			err := codecs.JSON.Unmarshal([]byte(serialized), &actual)
			assert.Error(t, err)
		})
	}
}

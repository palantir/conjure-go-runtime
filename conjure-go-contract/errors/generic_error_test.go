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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/palantir/conjure-go-runtime/conjure-go-contract/errors"
)

func TestError_Error(t *testing.T) {
	err := errors.NewError(
		errors.MustErrorType(errors.Timeout, "MyApplication:DatabaseTimeout"),
		errors.SafeParam("ttl", "10s"),
	)
	assert.EqualError(t, err, fmt.Sprintf("TIMEOUT MyApplication:DatabaseTimeout (%s)", err.InstanceID()))
}

func TestError_CodecsJSONEscapesHTML(t *testing.T) {
	e := errors.NewError(
		errors.MustErrorType(errors.Timeout, "MyApplication:Timeout"),
		errors.SafeParam("htmlKey", "something&something"),
	)

	marshalledError, err := codecs.JSON.Marshal(e)
	assert.NoError(t, err)
	assert.Regexp(t, `something&something`, string(marshalledError))
}

func TestError_NewError_Then_MarshalJSON_Then_UnmarshalJSON_And_Unpack(t *testing.T) {
	e := errors.NewError(
		errors.MustErrorType(errors.Timeout, "MyApplication:Timeout"),
		errors.SafeParam("safeKey", "safeValue"),
		errors.UnsafeParam("unsafeKey", "unsafeValue"),
	)

	marshalledError, err := codecs.JSON.Marshal(e)
	assert.NoError(t, err)

	var se errors.SerializableError
	err = codecs.JSON.Unmarshal(marshalledError, &se)
	assert.NoError(t, err)

	unpacked, err := errors.UnpackError(se)
	assert.NoError(t, err)
	assert.Equal(t, e, unpacked)
}

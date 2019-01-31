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
	"bytes"
	"strings"
	"testing"

	"github.com/palantir/pkg/uuid"
	"github.com/stretchr/testify/require"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
)

func TestPlainCodec(t *testing.T) {
	var str string
	var id uuid.UUID
	for _, test := range []struct {
		Name  string
		Data  string
		Value interface{}
	}{
		{
			Name:  "string",
			Data:  "hello world",
			Value: &str,
		},
		{
			Name:  "uuid",
			Data:  "12345678-1234-1234-1234-123456789000",
			Value: &id,
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			err := codecs.Plain.Decode(strings.NewReader(test.Data), test.Value)
			require.NoError(t, err)
			var buf bytes.Buffer
			err = codecs.Plain.Encode(&buf, test.Value)
			require.NoError(t, err)

			require.Equal(t, test.Data, buf.String())
		})
	}
}

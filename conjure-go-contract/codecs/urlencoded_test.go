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

package codecs_test

import (
	"bytes"
	"net/url"
	"testing"

	"github.com/palantir/conjure-go-runtime/conjure-go-contract/codecs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormURLEncoded_Decode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		urlValues url.Values
	}{
		{
			name: "decodes encoded url values",
			urlValues: url.Values{
				"a": []string{
					"x",
					"y",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readerContent := tc.urlValues.Encode()
			buff := bytes.NewBuffer([]byte(readerContent))
			var actualURLValues url.Values
			err := codecs.FormURLEncoded.Decode(buff, &actualURLValues)
			require.NoError(t, err)
			assert.Equal(t, tc.urlValues, actualURLValues)
		})
	}
}

func TestFormURLEncoded_Encode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		urlValues url.Values
	}{
		{
			name: "encodes encoded url values",
			urlValues: url.Values{
				"a": []string{
					"x",
					"y",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buff := &bytes.Buffer{}
			err := codecs.FormURLEncoded.Encode(buff, tc.urlValues)
			require.NoError(t, err)
			assert.Equal(t, tc.urlValues.Encode(), string(buff.Bytes()))
		})
	}
}

// Copyright (c) 2025 Palantir Technologies. All rights reserved.
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
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"math"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-contract/codecs"
	"github.com/palantir/pkg/safelong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var v2Codecs = map[string]codecs.Codec{
	"client": codecs.JSONClient,
	"server": codecs.JSONServer,
}

func TestCodecJSONv2_Marshal(t *testing.T) {
	for codecName, codec := range v2Codecs {
		t.Run(codecName, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				in   any
				want string
			}{
				{name: "nil slice", in: []string(nil), want: `[]`},
				{name: "nil map", in: map[string]int(nil), want: `{}`},
				{name: "nil slice field", in: struct{ A []int }{}, want: `{"A":[]}`},
				{name: "html not escaped", in: "<&>", want: `"<&>"`},
				{name: "map keys sorted", in: map[string]int{"b": 1, "a": 2, "c": 3}, want: `{"a":2,"b":1,"c":3}`},
				{name: "legacy omitempty", in: struct {
					I int     `json:"i,omitempty"`
					B bool    `json:"b,omitempty"`
					S *string `json:"s,omitempty"`
				}{S: new("")}, want: `{"s":""}`},
				{name: "json.Number", in: jsonv1.Number("9007199254740993"), want: `9007199254740993`},
				{name: "byte array as base64", in: [4]byte{1, 2, 3, 4}, want: `"AQIDBA=="`},
				{name: "safelong map key", in: map[safelong.SafeLong]safelong.SafeLong{1: 2}, want: `{"1":2}`},
				{name: "duration as nanos", in: time.Second, want: `1000000000`},
			} {
				t.Run(tc.name, func(t *testing.T) {
					out, err := codec.Marshal(tc.in)
					require.NoError(t, err)
					assert.Equal(t, tc.want, string(out))

					var buf bytes.Buffer
					require.NoError(t, codec.Encode(&buf, tc.in))
					assert.Equal(t, tc.want, buf.String())
				})
			}
			t.Run("invalid utf-8", func(t *testing.T) {
				_, err := codec.Marshal("a\xffb")
				var synErr *jsontext.SyntacticError
				require.ErrorAs(t, err, &synErr)
			})
			t.Run("duplicate names", func(t *testing.T) {
				_, err := codec.Marshal(jsontext.Value(`{"a":1,"a":2}`))
				var synErr *jsontext.SyntacticError
				require.ErrorAs(t, err, &synErr)
			})
			t.Run("encode error writes nothing", func(t *testing.T) {
				var buf bytes.Buffer
				err := codec.Encode(&buf, []any{"a", math.NaN()})
				var semErr *json.SemanticError
				require.ErrorAs(t, err, &semErr)
				assert.Empty(t, buf.String())
			})
		})
	}
}

func TestCodecJSONv2_Unmarshal(t *testing.T) {
	type named struct {
		Name string `json:"name"`
	}
	for codecName, codec := range v2Codecs {
		t.Run(codecName, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				in   string
				into func() any
				want any
			}{
				{name: "number in any is json.Number", in: `{"n":9007199254740993}`, into: func() any { return new(any) },
					want: map[string]any{"n": jsonv1.Number("9007199254740993")}},
				{name: "number in nested any is json.Number", in: `[[1.5]]`, into: func() any { return new(any) },
					want: []any{[]any{jsonv1.Number("1.5")}}},
				{name: "non-nil any decodes into its value", in: `{"name":"x"}`, into: func() any { var v any = &named{}; return &v },
					want: any(&named{Name: "x"})},
				{name: "null zeroes existing value", in: `null`, into: func() any { return new(5) },
					want: 0},
				{name: "duration as nanos", in: `1000000000`, into: func() any { return new(time.Duration) },
					want: time.Second},
				{name: "safelong map key", in: `{"1":2}`, into: func() any { return new(map[safelong.SafeLong]safelong.SafeLong) },
					want: map[safelong.SafeLong]safelong.SafeLong{1: 2}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					out := tc.into()
					require.NoError(t, codec.Unmarshal([]byte(tc.in), out))
					assert.Equal(t, tc.want, deref(out))

					out = tc.into()
					require.NoError(t, codec.Decode(strings.NewReader(tc.in), out))
					assert.Equal(t, tc.want, deref(out))
				})
			}
			for _, tc := range []struct {
				name string
				in   string
			}{
				{name: "duplicate names", in: `{"name":"x","name":"y"}`},
				{name: "invalid utf-8", in: "{\"name\":\"a\xffb\"}"},
				{name: "syntax", in: `{"name":`},
			} {
				t.Run("rejects "+tc.name, func(t *testing.T) {
					var synErr *jsontext.SyntacticError
					require.ErrorAs(t, codec.Unmarshal([]byte(tc.in), &named{}), &synErr)
					require.ErrorAs(t, codec.Decode(strings.NewReader(tc.in), &named{}), &synErr)
				})
			}
			for _, tc := range []struct {
				name string
				in   string
				into any
			}{
				{name: "newline in base64", in: `"AQID\nBA=="`, into: new([]byte)},
				{name: "comma in RFC 3339", in: `"2006-01-02T15:04:05,5Z"`, into: new(time.Time)},
			} {
				t.Run("rejects "+tc.name, func(t *testing.T) {
					var semErr *json.SemanticError
					require.ErrorAs(t, codec.Unmarshal([]byte(tc.in), tc.into), &semErr)
				})
			}
			t.Run("semantic error", func(t *testing.T) {
				var semErr *json.SemanticError
				require.ErrorAs(t, codec.Unmarshal([]byte(`{"name":1}`), &named{}), &semErr)
				assert.Equal(t, jsontext.Pointer("/name"), semErr.JSONPointer)
			})
			t.Run("trailing data", func(t *testing.T) {
				in := `{"name":"x"} {"name":"y"}`
				var synErr *jsontext.SyntacticError
				require.ErrorAs(t, codec.Unmarshal([]byte(in), &named{}), &synErr)
				require.ErrorAs(t, codec.Decode(strings.NewReader(in), &named{}), &synErr)
			})
			t.Run("decode reads to EOF", func(t *testing.T) {
				r := strings.NewReader(`{"name":"x"}  `)
				require.NoError(t, codec.Decode(iotest.OneByteReader(r), &named{}))
				assert.Zero(t, r.Len())
			})
		})
	}
	t.Run("unknown fields", func(t *testing.T) {
		// Names match case-sensitively, so NAME is an unknown field.
		in := `{"name":"x","NAME":"y"}`
		var out named
		require.NoError(t, codecs.JSONClient.Unmarshal([]byte(in), &out))
		assert.Equal(t, named{Name: "x"}, out)
		require.NoError(t, codecs.JSONClient.Decode(strings.NewReader(in), &out))

		var semErr *json.SemanticError
		require.ErrorAs(t, codecs.JSONServer.Unmarshal([]byte(in), &named{}), &semErr)
		assert.Equal(t, jsontext.Pointer("/NAME"), semErr.JSONPointer)
		require.ErrorAs(t, codecs.JSONServer.Decode(strings.NewReader(in), &named{}), &semErr)
	})
}

func TestCodecJSONWithOptions_OverrideDefaults(t *testing.T) {
	codec := codecs.JSONWithOptions(jsonv1.OmitEmptyWithLegacySemantics(false), jsonv1.FormatDurationAsNano(false))
	out, err := codec.Marshal(struct {
		S *string `json:"s,omitempty"`
	}{S: new("")})
	require.NoError(t, err)
	assert.Equal(t, `{}`, string(out))

	_, err = codec.Marshal(time.Second)
	var semErr *json.SemanticError
	require.ErrorAs(t, err, &semErr)
}

func deref(ptr any) any {
	return reflect.ValueOf(ptr).Elem().Interface()
}

// TestCodecJSON_MatchesV1 checks JSON against encoding/json configured as github.com/palantir/pkg/safejson did.
func TestCodecJSON_MatchesV1(t *testing.T) {
	for _, in := range []any{
		[]string(nil),
		map[string]int(nil),
		"<&>",
		"a\xffb",
		" ",
		map[string]int{"b": 1, "a": 2},
		[4]byte{1, 2, 3, 4},
		struct {
			I int `json:"i,omitempty"`
		}{},
		time.Second,
		math.NaN(),
	} {
		var want bytes.Buffer
		enc := jsonv1.NewEncoder(&want)
		enc.SetEscapeHTML(false)
		wantErr := enc.Encode(in)

		var got bytes.Buffer
		gotErr := codecs.JSON.Encode(&got, in)
		assert.Equal(t, want.String(), got.String(), "Encode %#v", in)
		assert.Equal(t, wantErr == nil, gotErr == nil, "Encode %#v: %v", in, gotErr)

		marshaled, err := codecs.JSON.Marshal(in)
		if wantErr != nil {
			var unsupported *jsonv1.UnsupportedValueError
			assert.ErrorAs(t, err, &unsupported)
		} else {
			assert.Equal(t, strings.TrimSuffix(want.String(), "\n"), string(marshaled))
		}
	}
	type named struct {
		Name string `json:"name"`
	}
	for _, in := range []string{
		`{"NAME":"x"}`,
		`{"name":"x","name":"y"}`,
		"{\"name\":\"a\xffb\"}",
		`{"name":"x"} trailing`,
		`{"name":1}`,
		`{"name":`,
		``,
	} {
		var want named
		dec := jsonv1.NewDecoder(strings.NewReader(in))
		dec.UseNumber()
		wantErr := dec.Decode(&want)

		var got named
		gotErr := codecs.JSON.Unmarshal([]byte(in), &got)
		assert.Equal(t, want, got, "Unmarshal %q", in)
		assert.Equal(t, wantErr == nil, gotErr == nil, "Unmarshal %q: %v", in, gotErr)
	}
	var got any
	require.NoError(t, codecs.JSON.Unmarshal([]byte(`{"n":9007199254740993}`), &got))
	assert.Equal(t, map[string]any{"n": jsonv1.Number("9007199254740993")}, got)
}

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

package json2

import (
	"bytes"
	stdjson "encoding/json"
	"io"
	"math"
	"strconv"

	"github.com/go-json-experiment/json"
	"github.com/go-json-experiment/json/jsontext"
	werror "github.com/palantir/witchcraft-go-error"
)

// ClientCodec implements conjure-go-runtime's Codec interface.
// It ignores unknown struct members.
var ClientCodec codecJSONv2ClientDecoder

// ServerCodec implements conjure-go-runtime's Codec interface.
// It rejects unknown struct members.
var ServerCodec codecJSONv2ServerDecoder

var (
	marshalOptions = json.JoinOptions(
		json.Deterministic(true),
		jsontext.AllowDuplicateNames(true),
		json.WithMarshalers(json.JoinMarshalers(
			// marshals a json.Number as-is, since this type is not recognized by the json v2 encoder and gets quoted as a string.
			json.MarshalFunc(func(number stdjson.Number) ([]byte, error) { return []byte(number), nil }),
			// marshals a json.RawMessage as-is, since this type is not recognized by the json v2 encoder and gets quoted as bytes.
			json.MarshalFunc(func(message stdjson.RawMessage) ([]byte, error) { return message, nil }),
			// marshals a float64 using "Infinity" and "NaN" for infinity and NaN respectively.
			json.MarshalFunc(func(number float64) ([]byte, error) {
				switch {
				case math.IsNaN(number):
					return []byte("NaN"), nil
				case math.IsInf(number, 1):
					return []byte("Infinity"), nil
				case math.IsInf(number, -1):
					return []byte("-Infinity"), nil
				default:
					return strconv.AppendFloat(nil, number, 'f', -1, 64), nil
				}
			}),
		)))
	unmarshalOptions = json.JoinOptions(
		json.WithUnmarshalers(json.JoinUnmarshalers(
			// unmarshals a json.Number as-is, since this type is not recognized by the json v2 encoder and gets quoted as a string.
			json.UnmarshalFunc[*stdjson.Number](func(data []byte, number *stdjson.Number) error { return assign(number, stdjson.Number(data)) }),
			// unmarshals a json.RawMessage as-is, since this type is not recognized by the json v2 encoder and gets quoted as bytes.
			json.UnmarshalFunc[*stdjson.RawMessage](func(data []byte, message *stdjson.RawMessage) error { return assign(message, data) }),
			// unmarshals a float64 using "Infinity" and "NaN" for infinity and NaN respectively.
			json.UnmarshalFunc[*float64](func(data []byte, number *float64) error {
				switch {
				case bytes.Equal(data, []byte("NaN")):
					return assign(number, math.NaN())
				case bytes.Equal(data, []byte("Infinity")):
					return assign(number, math.Inf(1))
				case bytes.Equal(data, []byte("-Infinity")):
					return assign(number, math.Inf(-1))
				default:
					parsed, err := strconv.ParseFloat(string(data), 64)
					if err != nil {
						return werror.Convert(err)
					}
					return assign(number, parsed)
				}
			}),
		)))
	// serverUnmarshalOptions is a copy of unmarshalOptions with RejectUnknownMembers(true)
	serverUnmarshalOptions = json.JoinOptions(unmarshalOptions, json.RejectUnknownMembers(true))
)

type codecJSONv2Encoder struct{}

func (codecJSONv2Encoder) ContentType() string {
	return "application/json"
}

func (codecJSONv2Encoder) Encode(w io.Writer, v any) error {
	if err := json.MarshalWrite(w, v, marshalOptions); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func (codecJSONv2Encoder) Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v, marshalOptions)
	if err != nil {
		return nil, werror.Convert(err)
	}
	return data, nil
}

type codecJSONv2ClientDecoder struct{ codecJSONv2Encoder }

func (codecJSONv2ClientDecoder) Accept() string {
	return "application/json"
}

func (codecJSONv2ClientDecoder) Decode(r io.Reader, v any) error {
	if err := json.UnmarshalRead(r, *&v, unmarshalOptions); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func (codecJSONv2ClientDecoder) Unmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, *&v, unmarshalOptions); err != nil {
		return werror.Convert(err)
	}
	return nil
}

type codecJSONv2ServerDecoder struct{ codecJSONv2Encoder }

func (codecJSONv2ServerDecoder) Accept() string {
	return "application/json"
}

func (codecJSONv2ServerDecoder) Decode(r io.Reader, v any) error {
	if err := json.UnmarshalRead(r, *&v, serverUnmarshalOptions); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func (codecJSONv2ServerDecoder) Unmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, *&v, serverUnmarshalOptions); err != nil {
		return werror.Convert(err)
	}
	return nil
}

func assign[T any](receiver *T, data T) error {
	*receiver = data
	return nil
}

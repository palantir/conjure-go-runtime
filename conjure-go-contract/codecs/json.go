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

package codecs

import (
	"bytes"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"

	werror "github.com/palantir/witchcraft-go-error"
)

const (
	contentTypeJSON = "application/json"
)

// JSON codec encodes and decodes JSON with encoding/json (v1) semantics.
// On Decode and Unmarshal, it decodes a JSON number into an interface value as a json.Number, to account for large numbers.
// On Encode and Marshal, it does not escape HTML characters, which for bad reasons (as acknowledged by go team) v1 does by default.
var JSON Codec = codecJSON{json.JoinOptions(jsonv1.DefaultOptionsV1(), jsontext.EscapeForHTML(false), json.WithUnmarshalers(unmarshalAnyNumber))}

// JSONClient codec encodes and decodes JSON using encoding/json/v2 with behaviors that match the Conjure wire format:
//   - Nil slices and maps marshal as [] and {}.
//   - Encode and Marshal reject invalid UTF-8 in strings and duplicate names in raw values.
//   - Decode and Unmarshal match object names case-sensitively and reject duplicate names and invalid UTF-8.
//   - Decode and Unmarshal reject line breaks in base64 and time.Time values that deviate from RFC 3339.
//   - Decode reads to EOF and rejects data after the JSON value. Encode writes no trailing newline.
//   - Errors are v2 errors (json.SemanticError and jsontext.SyntacticError).
//
// It keeps the v1 behaviors that conjure-go types depend on, omitempty semantics and json.Number,
// and encodes time.Duration as nanoseconds like the JSON codec.
// It ignores unknown fields, so that servers can add fields to their responses.
var JSONClient = JSONWithOptions()

// JSONServer codec is JSONClient, except that Decode and Unmarshal reject unknown fields.
var JSONServer = JSONWithOptions(json.RejectUnknownMembers(true))

// JSONWithOptions returns a codec with the JSONClient defaults and opts applied after them, so opts override the defaults.
// Passing json.WithUnmarshalers replaces the json.Number unmarshaler.
func JSONWithOptions(opts ...json.Options) Codec {
	return codecJSONv2{json.JoinOptions(append([]json.Options{jsonV2Defaults}, opts...)...)}
}

var jsonV2Defaults = json.JoinOptions(
	// v2 omits a non-nil pointer to "", which conjure-go generates for optional<string>.
	jsonv1.OmitEmptyWithLegacySemantics(true),
	json.Deterministic(true),
	jsonv1.FormatDurationAsNano(true),
	json.WithUnmarshalers(unmarshalAnyNumber),
)

// unmarshalAnyNumber is the equivalent of json.Decoder.UseNumber, which v2 does not expose as an option.
var unmarshalAnyNumber = json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *any) error {
	// A non-nil interface holds a value to decode into, such as a pointer.
	if *v != nil || dec.PeekKind() != '0' {
		return errors.ErrUnsupported
	}
	number, err := dec.ReadValue()
	if err != nil {
		return err
	}
	*v = jsonv1.Number(number)
	return nil
})

type codecJSON struct {
	options json.Options
}

func (codecJSON) ContentType() string {
	return contentTypeJSON
}

func (codecJSON) Accept() string {
	return contentTypeJSON
}

// Encode writes v and a trailing newline to w. Like jsonv1.Encoder, it writes nothing if marshaling fails.
func (c codecJSON) Encode(w io.Writer, v any) error {
	data, err := c.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return werror.Convert(err)
}

func (c codecJSON) Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v, c.options)
	return data, werror.Convert(err)
}

// Decode reads one JSON value from r and ignores any data after it, like jsonv1.Decoder.
func (c codecJSON) Decode(r io.Reader, v any) error {
	return werror.Convert(json.UnmarshalDecode(jsontext.NewDecoder(r, c.options), v))
}

// Unmarshal ignores any data after the first JSON value.
func (c codecJSON) Unmarshal(data []byte, v any) error {
	return c.Decode(bytes.NewReader(data), v)
}

type codecJSONv2 struct {
	options json.Options
}

func (codecJSONv2) ContentType() string {
	return contentTypeJSON
}

func (codecJSONv2) Accept() string {
	return contentTypeJSON
}

// Encode writes v to w. It writes nothing if marshaling fails.
func (c codecJSONv2) Encode(w io.Writer, v any) error {
	data, err := c.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return werror.Convert(err)
}

func (c codecJSONv2) Marshal(v any) ([]byte, error) {
	data, err := json.Marshal(v, c.options)
	return data, werror.Convert(err)
}

// Decode reads r to EOF, which must hold exactly one JSON value.
func (c codecJSONv2) Decode(r io.Reader, v any) error {
	return werror.Convert(json.UnmarshalRead(r, v, c.options))
}

func (c codecJSONv2) Unmarshal(data []byte, v any) error {
	return werror.Convert(json.Unmarshal(data, v, c.options))
}

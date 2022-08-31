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
	"encoding/json"
	"io"

	"github.com/palantir/pkg/safejson"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	contentTypeJSON = "application/json"
)

// JSON codec encodes and decodes JSON requests and responses using github.com/palantir/pkg/safejson.
// On Decode, it sets UseNumber on the json.Decoder to account for large numbers.
// On Encode, we disable HTML escaping, which for bad reasons (as acknowledged by go team), is default-enabled.
var JSON Codec = codecJSON{}

type codecJSON struct{}

func (codecJSON) Accept() string {
	return contentTypeJSON
}

func (codecJSON) Decode(r io.Reader, v interface{}) error {
	if decoder, ok := v.(jsonDecoder); ok {
		err := decoder.DecodeJSON(r)
		return werror.Wrap(err, "DecodeJSON")
	}
	if unmarshaler, ok := v.(json.Unmarshaler); ok {
		data, err := io.ReadAll(r)
		if err != nil {
			return werror.Wrap(err, "read failed")
		}
		err = unmarshaler.UnmarshalJSON(data)
		return werror.Wrap(err, "UnmarshalJSON")
	}
	if err := safejson.Decoder(r).Decode(v); err != nil {
		return werror.Wrap(err, "json.Decode")
	}
	return nil
}

func (c codecJSON) Unmarshal(data []byte, v interface{}) error {
	if unmarshaler, ok := v.(json.Unmarshaler); ok {
		err := unmarshaler.UnmarshalJSON(data)
		return werror.Wrap(err, "UnmarshalJSON")
	}
	if decoder, ok := v.(jsonDecoder); ok {
		err := decoder.DecodeJSON(bytes.NewReader(data))
		return werror.Wrap(err, "DecodeJSON")
	}
	if err := safejson.Unmarshal(data, v); err != nil {
		return werror.Wrap(err, "json.Unmarshal")
	}
	return nil
}

func (codecJSON) ContentType() string {
	return contentTypeJSON
}

func (codecJSON) Encode(w io.Writer, v interface{}) error {
	if encoder, ok := v.(jsonEncoder); ok {
		err := encoder.EncodeJSON(w)
		return werror.Wrap(err, "EncodeJSON")
	}
	if marshaler, ok := v.(json.Marshaler); ok {
		out, err := marshaler.MarshalJSON()
		if err != nil {
			return werror.Wrap(err, "MarshalJSON")
		}
		_, err = w.Write(out)
		return werror.Wrap(err, "write failed")
	}
	err := safejson.Encoder(w).Encode(v)
	return werror.Wrap(err, "json.Encode")
}

func (c codecJSON) Marshal(v interface{}) ([]byte, error) {
	if marshaler, ok := v.(json.Marshaler); ok {
		out, err := marshaler.MarshalJSON()
		return out, werror.Wrap(err, "MarshalJSON")
	}
	if encoder, ok := v.(jsonEncoder); ok {
		data := bytes.NewBuffer(nil)
		err := encoder.EncodeJSON(data)
		return data.Bytes(), werror.Wrap(err, "EncodeJSON")
	}
	out, err := safejson.Marshal(v)
	return out, werror.Wrap(err, "json.Marshal")
}

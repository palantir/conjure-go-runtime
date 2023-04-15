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
	switch vv := v.(type) {
	case jsonDecoder:
		return werror.Convert(vv.DecodeJSON(r))
	case json.Unmarshaler:
		data, err := io.ReadAll(r)
		if err != nil {
			return werror.Convert(err)
		}
		return werror.Convert(vv.UnmarshalJSON(data))
	default:
		return werror.Convert(safejson.Decoder(r).Decode(v))
	}
}

func (c codecJSON) Unmarshal(data []byte, v interface{}) error {
	switch vv := v.(type) {
	case json.Unmarshaler:
		return werror.Convert(vv.UnmarshalJSON(data))
	case jsonDecoder:
		return werror.Convert(vv.DecodeJSON(bytes.NewReader(data)))
	default:
		return werror.Convert(safejson.Unmarshal(data, v))
	}
}

func (codecJSON) ContentType() string {
	return contentTypeJSON
}

func (codecJSON) Encode(w io.Writer, v interface{}) error {
	switch vv := v.(type) {
	case jsonEncoder:
		return werror.Convert(vv.EncodeJSON(w))
	case json.Marshaler:
		out, err := vv.MarshalJSON()
		if err != nil {
			return werror.Convert(err)
		}
		_, err = w.Write(out)
		return werror.Convert(err)
	default:
		return werror.Convert(safejson.Encoder(w).Encode(v))
	}
}

func (c codecJSON) Marshal(v interface{}) ([]byte, error) {
	switch vv := v.(type) {
	case json.Marshaler:
		out, err := vv.MarshalJSON()
		return out, werror.Convert(err)
	case jsonEncoder:
		data := bytes.NewBuffer(nil) // TODO: Use bytesbuffer pool
		err := vv.EncodeJSON(data)
		return data.Bytes(), werror.Convert(err)
	default:
		out, err := safejson.Marshal(v)
		return out, werror.Convert(err)
	}
}

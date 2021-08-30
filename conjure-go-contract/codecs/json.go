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
	"encoding/json"
	"fmt"
	"io"

	"github.com/palantir/pkg/safejson"
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
	if unmarshaler, ok := v.(json.Unmarshaler); ok {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		return unmarshaler.UnmarshalJSON(data)
	} else if unmarshalFunc, ok := v.(func([]byte) error); ok {
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		return unmarshalFunc(data)
	}
	if err := safejson.Decoder(r).Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON-encoded value: %s", err.Error())
	}
	return nil
}

func (c codecJSON) Unmarshal(data []byte, v interface{}) error {
	if unmarshaler, ok := v.(json.Unmarshaler); ok {
		return unmarshaler.UnmarshalJSON(data)
	} else if unmarshalFunc, ok := v.(func([]byte) error); ok {
		return unmarshalFunc(data)
	}
	return safejson.Unmarshal(data, v)
}

func (codecJSON) ContentType() string {
	return contentTypeJSON
}

func (codecJSON) Encode(w io.Writer, v interface{}) error {
	if marshaler, ok := v.(json.Marshaler); ok {
		data, err := marshaler.MarshalJSON()
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	}
	if err := safejson.Encoder(w).Encode(v); err != nil {
		return fmt.Errorf("failed to JSON-encode value: %s", err.Error())
	}
	return nil
}

func (c codecJSON) Marshal(v interface{}) ([]byte, error) {
	if marshaler, ok := v.(json.Marshaler); ok {
		return marshaler.MarshalJSON()
	}
	return safejson.Marshal(v)
}

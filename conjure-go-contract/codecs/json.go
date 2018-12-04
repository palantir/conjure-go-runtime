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
	"fmt"
	"io"
)

const (
	contentTypeJSON = "application/json"
)

// JSON codec encodes and decodes JSON requests and responses.
// On Decode, it sets UseNumber on the json.Decoder to account for large numbers.
// On Encode, we disable HTML escaping, which for bad reasons (as acknowledged by go team), is default-enabled.
var JSON Codec = codecJSON{}

type codecJSON struct{}

func (codecJSON) Accept() string {
	return contentTypeJSON
}

func (codecJSON) Decode(r io.Reader, v interface{}) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("failed to decode JSON-encoded value: %s", err.Error())
	}
	return nil
}

func (c codecJSON) Unmarshal(data []byte, v interface{}) error {
	return c.Decode(bytes.NewBuffer(data), v)
}

func (codecJSON) ContentType() string {
	return contentTypeJSON
}

func (codecJSON) Encode(w io.Writer, v interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("failed to JSON-encode value: %s", err.Error())
	}
	return nil
}

func (c codecJSON) Marshal(v interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	err := c.Encode(&buffer, v)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

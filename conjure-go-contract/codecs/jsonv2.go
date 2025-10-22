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
	"io"

	"github.com/go-json-experiment/json"
)

// JSONv2 codec encodes and decodes json using "github.com/go-json-experiment/json", soon to be encoding/json/v2.
var JSONv2 Codec = codecJSONv2{}

type codecJSONv2 struct{}

func (codecJSONv2) Accept() string                     { return contentTypeJSON }
func (codecJSONv2) Decode(r io.Reader, v any) error    { return json.UnmarshalRead(r, v) }
func (codecJSONv2) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func (codecJSONv2) ContentType() string             { return contentTypeJSON }
func (codecJSONv2) Encode(w io.Writer, v any) error { return json.MarshalWrite(w, v) }
func (codecJSONv2) Marshal(v any) ([]byte, error)   { return json.Marshal(v) }

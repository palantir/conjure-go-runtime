// Copyright (c) 2022 Palantir Technologies. All rights reserved.
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
)

// jsonDecoder is an interface which may be implemented by objects passed to the Decode and Unmarshal methods.
// If implemented, the standard json.Decoder is bypassed.
type jsonDecoder interface {
	DecodeJSON(r io.Reader) error
}

// JSONDecoderFunc is an alias type which implements jsonDecoder.
// It can be used to declare anonymous/dynamic unmarshal logic.
type JSONDecoderFunc func(r io.Reader) error

func (f JSONDecoderFunc) DecodeJSON(r io.Reader) error {
	return f(r)
}

// JSONUnmarshalFunc is an alias type which implements json.Unmarshaler.
// It can be used to declare anonymous/dynamic unmarshal logic.
type JSONUnmarshalFunc func([]byte) error

func (f JSONUnmarshalFunc) UnmarshalJSON(data []byte) error {
	return f(data)
}

// jsonEncoder is an interface which may be implemented by objects passed to the Encode and Marshal methods.
// If implemented, the standard json.Encoder is bypassed.
type jsonEncoder interface {
	EncodeJSON(w io.Writer) error
}

// JSONEncoderFunc is an alias type which implements jsonEncoder.
// It can be used to declare anonymous/dynamic marshal logic.
type JSONEncoderFunc func(w io.Writer) error

func (f JSONEncoderFunc) EncodeJSON(w io.Writer) error {
	return f(w)
}

// JSONMarshalFunc is an alias type which implements json.Marshaler.
// It can be used to declare anonymous/dynamic marshal logic.
type JSONMarshalFunc func() ([]byte, error)

func (f JSONMarshalFunc) MarshalJSON() ([]byte, error) {
	return f()
}

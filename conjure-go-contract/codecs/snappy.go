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
	"bytes"
	"io"

	"github.com/golang/snappy"
)

var _ Codec = codecSNAPPY{}

// SNAPPY wraps an existing Codec and uses snappy for compression and decompression.
func SNAPPY(codec Codec) Codec {
	return &codecSNAPPY{contentCodec: codec}
}

type codecSNAPPY struct {
	contentCodec Codec
}

func (c codecSNAPPY) Accept() string {
	return c.contentCodec.Accept()
}

func (c codecSNAPPY) Decode(r io.Reader, v interface{}) error {
	snappyReader := snappy.NewReader(r)
	return c.contentCodec.Decode(snappyReader, v)
}

func (c codecSNAPPY) Unmarshal(data []byte, v interface{}) error {
	return c.Decode(bytes.NewBuffer(data), v)
}

func (c codecSNAPPY) ContentType() string {
	return c.contentCodec.ContentType()
}

func (c codecSNAPPY) Encode(w io.Writer, v interface{}) (err error) {
	snappyWriter := snappy.NewWriter(w)
	defer func() {
		if closeErr := snappyWriter.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	return c.contentCodec.Encode(snappyWriter, v)
}

func (c codecSNAPPY) Marshal(v interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	err := c.Encode(&buffer, v)
	if err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'}), nil
}

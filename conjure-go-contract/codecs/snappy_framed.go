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

var _ Codec = codecSNAPPYFRAMED{}

// SnappyFramed wraps an existing Codec and uses snappy with framing for
// compression and decompression using github.com/golang/snappy.
// The Content-Type of the encoded data is expected to be the following:
//
// Ref: https://github.com/google/snappy/blob/main/framing_format.txt
func SnappyFramed(codec Codec) Codec {
	return &codecSNAPPYFRAMED{contentCodec: codec}
}

type codecSNAPPYFRAMED struct {
	contentCodec Codec
}

func (c codecSNAPPYFRAMED) Accept() string {
	return c.contentCodec.Accept()
}

func (c codecSNAPPYFRAMED) Decode(r io.Reader, v interface{}) (err error) {
	return c.contentCodec.Decode(snappy.NewReader(r), v)
}

func (c codecSNAPPYFRAMED) Unmarshal(data []byte, v interface{}) error {
	return c.Decode(bytes.NewBuffer(data), v)
}

func (c codecSNAPPYFRAMED) ContentType() string {
	return c.contentCodec.ContentType()
}

func (c codecSNAPPYFRAMED) Encode(w io.Writer, v interface{}) (err error) {
	snappyWriter := snappy.NewBufferedWriter(w)
	defer func() {
		if closeErr := snappyWriter.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()

	return c.contentCodec.Encode(snappyWriter, v)
}

func (c codecSNAPPYFRAMED) Marshal(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	if err := c.Encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

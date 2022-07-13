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

var _ Codec = codecSnappyFramed{}

// SnappyFramed wraps an existing Codec and uses snappy with framing for
// compression and decompression using github.com/golang/snappy. The framing
// format is optional for Snappy compressors and decompressor; it is not
// part of the Snappy core specification.
// Ref https://github.com/google/snappy/blob/main/framing_format.txt
func SnappyFramed(codec Codec) Codec {
	return &codecSnappyFramed{contentCodec: codec}
}

type codecSnappyFramed struct {
	contentCodec Codec
}

func (c codecSnappyFramed) Accept() string {
	return c.contentCodec.Accept()
}

func (c codecSnappyFramed) Decode(r io.Reader, v interface{}) error {
	snappyReader := snappy.NewReader(r)
	return c.contentCodec.Decode(snappyReader, v)
}

func (c codecSnappyFramed) Unmarshal(data []byte, v interface{}) error {
	return c.Decode(bytes.NewBuffer(data), v)
}

func (c codecSnappyFramed) ContentType() string {
	return c.contentCodec.ContentType()
}

func (c codecSnappyFramed) Encode(w io.Writer, v interface{}) (err error) {
	snappyWriter := snappy.NewBufferedWriter(w)
	defer func() {
		if closeErr := snappyWriter.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if err := c.contentCodec.Encode(snappyWriter, v); err != nil {
		return err
	}
	return snappyWriter.Flush()
}

func (c codecSnappyFramed) Marshal(v interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	err := c.Encode(&buffer, v)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

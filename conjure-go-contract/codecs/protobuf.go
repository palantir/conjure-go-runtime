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
	"io/ioutil"

	protoio "github.com/gogo/protobuf/io"
	"github.com/gogo/protobuf/proto"
	werror "github.com/palantir/witchcraft-go-error"
)

const (
	contentTypeProtobuf = "Application/x-protobuf"
)

// Protobuf codec encodes and decodes protobuf requests and responses using
// github.com/golang/protobuf/proto.
var Protobuf Codec = codecProtobuf{}

type codecProtobuf struct{}

func (codecProtobuf) Accept() string {
	return contentTypeProtobuf
}

func (codecProtobuf) Decode(r io.Reader, v interface{}) error {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	return Protobuf.Unmarshal(data, v)
}

func (c codecProtobuf) Unmarshal(data []byte, v interface{}) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return werror.Error("failed to decode protobuf data from type which does not implement proto.Message")
	}
	buf := proto.NewBuffer(data)
	return buf.Unmarshal(msg)
}

func (codecProtobuf) ContentType() string {
	return contentTypeProtobuf
}

func (codecProtobuf) Encode(w io.Writer, v interface{}) error {
	msg, ok := v.(proto.Message)
	if !ok {
		return werror.Error("failed to encode protobuf data from type which does not implement proto.Message")
	}
	protoWriter := protoio.NewFullWriter(w)
	return protoWriter.WriteMsg(msg)
}

func (c codecProtobuf) Marshal(v interface{}) ([]byte, error) {
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, werror.Error("failed to encode protobuf data from type which does not implement proto.Message")
	}
	var buf proto.Buffer
	if err := buf.Marshal(msg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

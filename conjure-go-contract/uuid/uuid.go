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

package uuid

import (
	"encoding"
	"fmt"

	googleuuid "github.com/google/uuid"
)

func NewUUID() UUID {
	return [16]byte(googleuuid.New())
}

var (
	_ fmt.Stringer               = UUID{}
	_ encoding.TextMarshaler     = UUID{}
	_ encoding.TextUnmarshaler   = &UUID{}
	_ encoding.BinaryMarshaler   = UUID{}
	_ encoding.BinaryUnmarshaler = &UUID{}
)

// UUID (universally unique identifier) is a 128-bit number used to
// identify information in computer systems as defined in RFC 4122.
type UUID [16]byte

// String returns uuid string representation "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
// or "" if uuid is invalid.
func (uuid UUID) String() string {
	return googleuuid.UUID(uuid).String()
}

func (uuid UUID) MarshalText() ([]byte, error) {
	return googleuuid.UUID(uuid).MarshalText()
}

func (uuid *UUID) UnmarshalText(data []byte) error {
	return (*googleuuid.UUID)(uuid).UnmarshalText(data)
}

func (uuid UUID) MarshalBinary() ([]byte, error) {
	return googleuuid.UUID(uuid).MarshalBinary()
}

func (uuid *UUID) UnmarshalBinary(data []byte) error {
	return (*googleuuid.UUID)(uuid).UnmarshalBinary(data)
}

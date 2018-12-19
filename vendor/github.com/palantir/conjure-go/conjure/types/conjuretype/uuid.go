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

package conjuretype

import (
	"encoding"
	"fmt"

	"github.com/palantir/conjure-go/conjure/types/conjuretype/internal/uuid"
)

func NewUUID() UUID {
	return [16]byte(uuid.New())
}

var (
	_ fmt.Stringer             = UUID{}
	_ encoding.TextMarshaler   = UUID{}
	_ encoding.TextUnmarshaler = &UUID{}
)

// UUID (universally unique identifier) is a 128-bit number used to
// identify information in computer systems as defined in RFC 4122.
type UUID [16]byte

func ParseUUID(s string) (UUID, error) {
	var u UUID
	err := (&u).UnmarshalText([]byte(s))
	return u, err
}

// String returns uuid string representation "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
// or "" if uuid is invalid.
func (u UUID) String() string {
	return uuid.UUID(u).String()
}

// MarshalText implements encoding.TextMarshaler.
func (u UUID) MarshalText() ([]byte, error) {
	return uuid.UUID(u).MarshalText()
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *UUID) UnmarshalText(data []byte) error {
	return (*uuid.UUID)(u).UnmarshalText(data)
}

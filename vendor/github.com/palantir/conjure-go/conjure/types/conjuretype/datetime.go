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
	"strings"
	"time"
)

type DateTime time.Time

func (d DateTime) String() string {
	return time.Time(d).Format(time.RFC3339Nano)
}

// MarshalText implements encoding.TextMarshaler (used by encoding/json and others).
func (d DateTime) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler (used by encoding/json and others).
func (d *DateTime) UnmarshalText(b []byte) error {
	val := string(b)

	// Conjure supports DateTime inputs that end with an optional zone identifier enclosed in square brackets
	// (for example, "2017-01-02T04:04:05.000000000+01:00[Europe/Berlin]"). If the input string ends in a ']' and
	// contains a '[', parse the string up to '['.
	if strings.HasSuffix(val, "]") {
		if openBracketIdx := strings.LastIndex(val, "["); openBracketIdx != -1 {
			val = val[:openBracketIdx]
		}
	}

	timeVal, err := time.Parse(time.RFC3339Nano, val)
	if err != nil {
		return err
	}
	*d = DateTime(timeVal)

	return nil
}

func ParseDateTime(s string) (DateTime, error) {
	// Conjure supports DateTime inputs that end with an optional zone identifier enclosed in square brackets
	// (for example, "2017-01-02T04:04:05.000000000+01:00[Europe/Berlin]"). If the input string ends in a ']' and
	// contains a '[', parse the string up to '['.
	if strings.HasSuffix(s, "]") {
		if openBracketIdx := strings.LastIndex(s, "["); openBracketIdx != -1 {
			s = s[:openBracketIdx]
		}
	}

	timeVal, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return DateTime(time.Time{}), err
	}
	return DateTime(timeVal), nil
}

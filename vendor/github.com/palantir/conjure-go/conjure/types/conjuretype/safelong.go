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
	"encoding/json"
	"fmt"
	"strconv"
)

const (
	safeIntVal = int64(1) << 53
	minVal     = -safeIntVal + 1
	maxVal     = safeIntVal - 1
)

type SafeLong int64

func NewSafeLong(val int64) (SafeLong, error) {
	if err := validate(val); err != nil {
		return 0, err
	}
	return SafeLong(val), nil
}

func ParseSafeLong(s string) (SafeLong, error) {
	i, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return NewSafeLong(i)
}

func (s *SafeLong) UnmarshalJSON(b []byte) error {
	var val int64
	if err := json.Unmarshal(b, &val); err != nil {
		return err
	}

	newVal, err := NewSafeLong(val)
	if err != nil {
		return err
	}
	*s = newVal

	return nil
}

func (s SafeLong) MarshalJSON() ([]byte, error) {
	if err := validate(int64(s)); err != nil {
		return nil, err
	}
	return json.Marshal(int64(s))
}

func validate(val int64) error {
	if val < minVal || val > maxVal {
		return fmt.Errorf("%d is not a valid value for a SafeLong as it is not safely representable in Javascript: must be between %d and %d", val, minVal, maxVal)
	}
	return nil
}

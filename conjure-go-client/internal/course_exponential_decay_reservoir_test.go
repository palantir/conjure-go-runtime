// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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

package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecayToZero(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	assert.InDelta(t, 0.0, r.Get(), 0.001)
	r.Update(1)
	assert.InDelta(t, 1.0, r.Get(), 0.001)
	time = 300000
	assert.InDelta(t, 0.0, r.Get(), 0.001)
}

func TestDecayByHalf(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	r.Update(2)
	assert.InDelta(t, 2.0, r.Get(), 0.001)
	time = 10
	assert.InDelta(t, 1.0, r.Get(), 0.001)
	time = 20
	assert.InDelta(t, 0.5, r.Get(), 0.001)
}

func TestIntermediateDecay(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	r.Update(100)
	assert.InDelta(t, 100.0, r.Get(), 0.001)
	time = 2
	assert.Less(t, r.Get(), 100.0)
	assert.Greater(t, r.Get(), 50.0)
	time = 10
	assert.InDelta(t, 50.0, r.Get(), 0.001)
}

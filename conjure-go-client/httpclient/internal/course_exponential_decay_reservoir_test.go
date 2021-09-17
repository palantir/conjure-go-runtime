package internal

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestDecayToZero(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	assert.InDelta(t, r.Get(), 0.0, 0.001)
	r.Update(1)
	assert.InDelta(t, r.Get(), 1.0, 0.001)
	time = 300000
	assert.InDelta(t, r.Get(), 0.0, 0.001)
}

func TestDecayByHalf(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	r.Update(2)
	assert.InDelta(t, r.Get(), 2.0, 0.001)
	time = 10
	assert.InDelta(t, r.Get(), 1.0, 0.001)
	time = 20
	assert.InDelta(t, r.Get(), 0.5, 0.001)
}

func TestIntermediateDecay(t *testing.T) {
	time := int64(0)
	clock := func() int64 {
		return time
	}
	r := NewCourseExponentialDecayReservoir(clock, 10)
	r.Update(100)
	assert.InDelta(t, r.Get(), 100.0, 0.001)
	time = 2
	assert.Less(t, r.Get(), 100.0)
	assert.Greater(t, r.Get(), 50.0)
	time = 10
	assert.InDelta(t, r.Get(), 50.0, 0.001)
}

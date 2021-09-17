package internal

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const (
	decaysPerHalfLife = 10
)

var (
	decayFactor = math.Pow(0.5, 1.0/decaysPerHalfLife)
)

type CourseExponentialDecayReservoir interface {
	Update(updates float64)
	Get() float64
}

var _ CourseExponentialDecayReservoir = (*reservoir)(nil)

type reservoir struct {
	value                    float64
	lastDecay                int64
	nanoClock                func() int64
	decayIntervalNanoseconds int64
	mu                       sync.Mutex
}

func NewCourseExponentialDecayReservoir(nanoClock func() int64, halfLife time.Duration) CourseExponentialDecayReservoir {
	return &reservoir{
		lastDecay:                nanoClock(),
		nanoClock:                nanoClock,
		decayIntervalNanoseconds: halfLife.Nanoseconds() / decaysPerHalfLife,
	}
}

func (r *reservoir) Update(updates float64) {
	r.decayIfNecessary()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = r.value + updates
}

func (r *reservoir) Get() float64 {
	r.decayIfNecessary()
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.value
}

func (r *reservoir) decayIfNecessary() {
	now := r.nanoClock()
	lastDecaySnapshot := r.lastDecay
	nanosSinceLastDecay := now - lastDecaySnapshot
	decays := nanosSinceLastDecay / r.decayIntervalNanoseconds
	// If CAS fails another thread will execute decay instead
	if decays > 0 && atomic.CompareAndSwapInt64(&r.lastDecay, lastDecaySnapshot, lastDecaySnapshot+decays*r.decayIntervalNanoseconds) {
		r.decay(decays)
	}
}

func (r *reservoir) decay(decayIterations int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.value = r.value * math.Pow(decayFactor, float64(decayIterations))
}

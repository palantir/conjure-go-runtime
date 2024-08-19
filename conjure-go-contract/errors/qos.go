package errors

import (
	"net/http"
	"strconv"
	"time"
)

type QOSResponse interface {
	error
	Status() int
	Header(http.Header)
	isQoS() // marker method
}

type QOSRetryOther struct {
	Location string
}

func (QOSRetryOther) Error() string {
	return "308 Retry Other"
}

func (QOSRetryOther) Status() int {
	return http.StatusPermanentRedirect
}

func (q QOSRetryOther) Header(h http.Header) {
	if q.Location != "" {
		h.Set("Location", q.Location)
	}
}

func (QOSRetryOther) isQoS() {}

type QOSThrottle struct {
	RetryAfter time.Duration
}

func (QOSThrottle) Error() string {
	return "429 Throttle"
}

func (QOSThrottle) Status() int {
	return http.StatusTooManyRequests
}

func (q QOSThrottle) Header(h http.Header) {
	if q.RetryAfter > 0 {
		h.Set("Retry-After", strconv.Itoa(int(q.RetryAfter/time.Second)))
	}
}

func (QOSThrottle) isQoS() {}

type QOSUnavailable struct{}

func (QOSUnavailable) Error() string {
	return "503 Unavailable"
}

func (QOSUnavailable) Status() int {
	return http.StatusServiceUnavailable
}

func (QOSUnavailable) Header(http.Header) {}

func (QOSUnavailable) isQoS() {}

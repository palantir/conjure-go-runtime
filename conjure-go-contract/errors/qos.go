// Copyright (c) 2024 Palantir Technologies. All rights reserved.
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

package errors

import (
	"net/http"
	"strconv"
	"time"

	werror "github.com/palantir/witchcraft-go-error"
	wparams "github.com/palantir/witchcraft-go-params"
)

type QOSResponseError interface {
	Error() string
	Status() int
	Header(http.Header)
	wparams.ParamStorer
	werror.Causer
	Unwrap() error
	isQoS() // marker method
}

// QOSRetryOther is an error type that represents a 308 Retry Other response.
// It may contain a Location header indicating the new location to which the request should be redirected.
type QOSRetryOther struct {
	Location string
	Err      error // optional underlying cause
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

func (QOSRetryOther) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusPermanentRedirect}
}

func (q QOSRetryOther) UnsafeParams() map[string]any {
	return map[string]any{"location": q.Location}
}

func (q QOSRetryOther) Cause() error {
	return q.Err
}

func (q QOSRetryOther) Unwrap() error {
	return q.Err
}

func (QOSRetryOther) isQoS() {}

// QOSThrottle is an error type that represents a 429 Throttle response.
// It may contain a Retry-After header indicating the number of seconds to wait before retrying the request.
type QOSThrottle struct {
	RetryAfter time.Duration
	RetryAt    time.Time
	Err        error // optional underlying cause
}

func QOSThrottleFromHeader(header http.Header) QOSThrottle {
	retryAfterStr := header.Get("Retry-After")
	if retryAfterStr == "" {
		return QOSThrottle{}
	}
	// Retry-After can be either a Date or a number of seconds; look for both.
	if retryAfterSec, err := strconv.Atoi(retryAfterStr); err == nil && retryAfterSec > 0 {
		return QOSThrottle{RetryAfter: time.Duration(retryAfterSec) * time.Second}
	}
	if retryAfterDuration, err := time.ParseDuration(retryAfterStr); err == nil && retryAfterDuration > 0 {
		return QOSThrottle{RetryAfter: retryAfterDuration}
	}
	if retryAfterDate, err := http.ParseTime(retryAfterStr); err == nil && !retryAfterDate.IsZero() {
		return QOSThrottle{RetryAt: retryAfterDate}
	}
	if retryAfterDate, err := time.Parse(time.RFC3339, retryAfterStr); err == nil && !retryAfterDate.IsZero() {
		return QOSThrottle{RetryAt: retryAfterDate}
	}
	// Unable to parse non-zero header as something we recognize...
	return QOSThrottle{}
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
	} else if !q.RetryAt.IsZero() {
		h.Set("Retry-After", q.RetryAt.UTC().Format(http.TimeFormat))
	}
}

func (QOSThrottle) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusTooManyRequests}
}

func (QOSThrottle) UnsafeParams() map[string]any {
	return map[string]any{}
}

func (q QOSThrottle) Cause() error {
	return q.Err
}

func (q QOSThrottle) Unwrap() error {
	return q.Err
}

func (QOSThrottle) isQoS() {}

// QOSUnavailable is an error type that represents a 503 Service Unavailable response.
type QOSUnavailable struct {
	Err error // optional underlying cause
}

func (QOSUnavailable) Error() string {
	return "503 Unavailable"
}

func (QOSUnavailable) Status() int {
	return http.StatusServiceUnavailable
}

func (QOSUnavailable) Header(http.Header) {}

func (QOSUnavailable) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusServiceUnavailable}
}

func (QOSUnavailable) UnsafeParams() map[string]any {
	return map[string]any{}
}

func (q QOSUnavailable) Cause() error {
	return q.Err
}

func (q QOSUnavailable) Unwrap() error {
	return q.Err
}

func (QOSUnavailable) isQoS() {}

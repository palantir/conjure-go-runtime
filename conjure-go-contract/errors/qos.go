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

	wparams "github.com/palantir/witchcraft-go-params"
)

type QOSResponse interface {
	error
	Status() int
	Header(http.Header)
	wparams.ParamStorer
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

func (QOSRetryOther) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusPermanentRedirect}
}

func (q QOSRetryOther) UnsafeParams() map[string]any {
	return map[string]any{"location": q.Location}
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

func (QOSThrottle) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusTooManyRequests}
}

func (QOSThrottle) UnsafeParams() map[string]any {
	return map[string]any{}
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

func (QOSUnavailable) SafeParams() map[string]any {
	return map[string]any{"statusCode": http.StatusServiceUnavailable}
}

func (QOSUnavailable) UnsafeParams() map[string]any {
	return map[string]any{}
}

func (QOSUnavailable) isQoS() {}

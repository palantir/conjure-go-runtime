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

package internal

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	cgrerrors "github.com/palantir/conjure-go-runtime/v2/conjure-go-contract/errors"
)

/* https://github.com/palantir/http-remoting#quality-of-service-retry-failover-throttling

Quality of service: retry, failover, throttling
http-remoting servers can use the QosException class to advertise the following conditions:

* throttle: Returns a Throttle exception indicating that the calling client should throttle its requests. The client may retry against an arbitrary node of this service.
* retryOther: Returns a RetryOther exception indicating that the calling client should retry against the given node of this service.
* unavailable: An exception indicating that (this node of) this service is currently unavailable and the client may try again at a later time, possibly against a different node of this service.

The QosExceptions have a stable mapping to HTTP status codes and response headers:

* throttle: 429 Too Many Requests, plus optional Retry-After header
* retryOther: 308 Permanent Redirect, plus Location header indicating the target host
* retryTemporaryRedirect: 307 Temporary Redirect, plus Location header indicating the target host
* unavailable: 503 Unavailable

http-remoting clients (both Retrofit2 and JaxRs) handle the above error codes and take the appropriate action:

* throttle: reschedule the request with a delay: either the indicated Retry-After period, or a configured exponential backoff
* retryOther: retry the request against the indicated service node; all request parameters and headers are maintained
* unavailable: retry the request on a different host after a configurable exponential delay

Connection errors (e.g., connection refused or DNS errors) yield a retry against a different node of the service.
Retries pick a target host by cycling through the list of URLs configured for a Service (see ClientConfiguration#uris).
Note that the "current" URL is maintained across calls; for example, if a first call yields a retryOther/308 redirect, then any subsequent calls will be made against that URL.
Similarly, if the first URL yields a DNS error and the retried call succeeds against the URL from the list, then subsequent calls are made against that URL.

The number of retries for 503 and connection errors can be configured via ClientConfiguration#maxNumRetries or ServiceConfiguration#maxNumRetries, defaulting to two (2) times the number of URIs provided in #uris.

*/

const (
	StatusCodeRetryOther             = http.StatusPermanentRedirect
	StatusCodeRetryTemporaryRedirect = http.StatusTemporaryRedirect
	StatusCodeThrottle               = http.StatusTooManyRequests
	StatusCodeUnavailable            = http.StatusServiceUnavailable
)

func isRetryOtherResponse(resp *http.Response, respErr error) (bool, *url.URL) {
	if respErr != nil {
		if retryOther := new(cgrerrors.QOSRetryOther); errors.As(respErr, retryOther) {
			if retryOther.Location != "" {
				return true, parseLocationURL(retryOther.Location)
			}
			return true, nil
		}
		if st, ok := StatusCodeFromError(respErr); ok && st == StatusCodeRetryOther || st == StatusCodeRetryTemporaryRedirect {
			return true, nil
		}
	}
	if resp == nil {
		return false, nil
	}
	if resp.StatusCode != StatusCodeRetryOther && resp.StatusCode != StatusCodeRetryTemporaryRedirect {
		return false, nil
	}
	location, err := resp.Location()
	if err != nil {
		return true, nil
	}
	return true, location
}

func parseLocationURL(locationStr string) *url.URL {
	if locationStr == "" {
		return nil
	}
	locationURL, err := url.Parse(locationStr)
	if err != nil {
		// Unable to parse location as something we recognize
		return nil
	}
	return locationURL
}

// isThrottleResponse returns true if the response a throttle response type. It
// also returns a duration after which the failed URI can be retried
func isThrottleResponse(resp *http.Response, respErr error) (bool, time.Duration) {
	if respErr != nil {
		if throttle := new(cgrerrors.QOSThrottle); errors.As(respErr, throttle) {
			if throttle.RetryAfter > 0 {
				return true, throttle.RetryAfter
			}
			if !throttle.RetryAt.IsZero() {
				return true, time.Until(throttle.RetryAt)
			}
			return true, 0
		}
		if st, ok := StatusCodeFromError(respErr); ok && st == StatusCodeThrottle {
			return true, 0
		}
	}
	if resp != nil && resp.StatusCode == StatusCodeThrottle {
		throttle := cgrerrors.QOSThrottleFromHeader(resp.Header)
		if throttle.RetryAfter > 0 {
			return true, throttle.RetryAfter
		}
		if !throttle.RetryAt.IsZero() {
			return true, time.Until(throttle.RetryAt)
		}
		return true, 0
	}
	return false, 0
}

func isUnavailableResponse(resp *http.Response, respErr error) bool {
	if respErr != nil {
		if errors.As(respErr, new(cgrerrors.QOSUnavailable)) {
			return true
		}
		if st, ok := StatusCodeFromError(respErr); ok && st == StatusCodeUnavailable {
			return true
		}
	} else if resp != nil && resp.StatusCode == StatusCodeUnavailable {
		return true
	}

	return false
}

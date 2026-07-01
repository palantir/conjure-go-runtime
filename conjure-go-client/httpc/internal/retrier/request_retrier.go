// Copyright (c) 2020 Palantir Technologies. All rights reserved.
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

package retrier

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpc/internal"
	"github.com/palantir/pkg/retry"
)

const (
	meshSchemePrefix = "mesh-"
)

// RequestRetrier manages URIs for an HTTP client, providing an API which determines whether requests should be retries
// and supplying the correct URL for the client to retry.
// In the case of servers in a service-mesh, requests will never be retried and the mesh URI will only be returned on the
// first call to GetNextURI
type RequestRetrier struct {
	currentURI    string
	retrier       retry.Retrier
	uris          []string
	offset        int
	relocatedURIs map[string]struct{}
	failedURIs    map[string]struct{}
	maxAttempts   int
	attemptCount  int
}

// NewRequestRetrier creates a new request retrier.
// Regardless of maxAttempts, mesh URIs will never be retried.
func NewRequestRetrier(uris []string, retrier retry.Retrier, maxAttempts int) *RequestRetrier {
	offset := 0
	return &RequestRetrier{
		currentURI:    uris[offset],
		retrier:       retrier,
		uris:          uris,
		offset:        offset,
		relocatedURIs: map[string]struct{}{},
		failedURIs:    map[string]struct{}{},
		maxAttempts:   maxAttempts,
		attemptCount:  0,
	}
}

func (r *RequestRetrier) attemptsRemaining() bool {
	// maxAttempts of 0 indicates no limit
	if r.maxAttempts == 0 {
		return true
	}
	return r.attemptCount < r.maxAttempts
}

// GetNextURI returns the next URI a client should use, or empty string if no suitable URI remaining to retry.
// isRelocated is true when the URI comes from a redirect's Location header. In this case, it already includes the request path.
func (r *RequestRetrier) GetNextURI(resp *http.Response, respErr error) (uri string, isRelocated bool) {
	defer func() {
		r.attemptCount++
	}()
	if r.attemptCount == 0 {
		// First attempt is always successful. Trigger the first retry so later calls have backoff
		// but ignore the returned value to ensure that the client can instrument the request even
		// if the context is done.
		r.retrier.Next()
		return r.removeMeshSchemeIfPresent(r.currentURI), false
	}
	if !r.attemptsRemaining() {
		// Retries exhausted
		return "", false
	}
	if r.isMeshURI(r.currentURI) {
		// Mesh uris don't get retried
		return "", false
	}
	retryFn := r.getRetryFn(resp, respErr)
	if retryFn == nil {
		// The previous response was not retryable
		return "", false
	}
	// Updates currentURI
	if !retryFn() {
		return "", false
	}
	return r.currentURI, r.isRelocatedURI(r.currentURI)
}

func (r *RequestRetrier) getRetryFn(resp *http.Response, respErr error) func() bool {
	errCode, _ := internal.StatusCodeFromError(respErr)
	if retryOther, _ := isThrottleResponse(resp, errCode); retryOther {
		// 429: throttle
		// Immediately backoff and select the next URI.
		// TODO(whickman): use the retry-after header once #81 is resolved
		return r.nextURIAndBackoff
	} else if isUnavailableResponse(resp, errCode) {
		// 503: go to next node
		return r.nextURIOrBackoff
	} else if shouldTryOther, otherURI := isRetryOtherResponse(resp, respErr, errCode); shouldTryOther {
		// 307 or 308: go to next node, or particular node if provided.
		if otherURI != nil {
			return func() bool {
				r.setURIAndResetBackoff(otherURI)
				return true
			}
		}
		return r.nextURIOrBackoff
	} else if errCode >= http.StatusBadRequest && errCode < http.StatusInternalServerError {
		return nil
	} else if resp == nil {
		// if we get a nil response, we can assume there is a problem with host and can move on to the next.
		return r.nextURIOrBackoff
	}
	return nil
}

func (r *RequestRetrier) setURIAndResetBackoff(otherURI *url.URL) {
	nextURI := otherURI.String()
	r.relocatedURIs[otherURI.String()] = struct{}{}
	r.retrier.Reset()
	r.currentURI = nextURI
}

// If lastURI was already marked failed, we perform a backoff as determined by the retrier before returning the next URI and its offset.
// Otherwise, we add lastURI to failedURIs and return the next URI and its offset immediately.
func (r *RequestRetrier) nextURIOrBackoff() bool {
	_, performBackoff := r.failedURIs[r.currentURI]
	r.markFailedAndMoveToNextURI()
	// If the URI has failed before, perform a backoff
	if performBackoff || len(r.uris) == 1 {
		return r.retrier.Next()
	}
	return true
}

// Marks the current URI as failed, gets the next URI, and performs a backoff as determined by the retrier.
func (r *RequestRetrier) nextURIAndBackoff() bool {
	r.markFailedAndMoveToNextURI()
	return r.retrier.Next()
}

func (r *RequestRetrier) markFailedAndMoveToNextURI() {
	r.failedURIs[r.currentURI] = struct{}{}
	nextURIOffset := (r.offset + 1) % len(r.uris)
	nextURI := r.uris[nextURIOffset]
	r.currentURI = nextURI
	r.offset = nextURIOffset
}

func (r *RequestRetrier) removeMeshSchemeIfPresent(uri string) string {
	if r.isMeshURI(uri) {
		return strings.Replace(uri, meshSchemePrefix, "", 1)
	}
	return uri
}

func (r *RequestRetrier) isMeshURI(uri string) bool {
	return strings.HasPrefix(uri, meshSchemePrefix)
}

func (r *RequestRetrier) isRelocatedURI(uri string) bool {
	_, relocatedURI := r.relocatedURIs[uri]
	return relocatedURI
}

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

func isRetryOtherResponse(resp *http.Response, err error, errCode int) (bool, *url.URL) {
	if errCode == StatusCodeRetryOther || errCode == StatusCodeRetryTemporaryRedirect {
		locationStr, ok := internal.LocationFromError(err)
		if !ok {
			return true, nil
		}
		return true, parseLocationURL(locationStr)
	}

	if resp == nil {
		return false, nil
	}
	if resp.StatusCode != StatusCodeRetryOther &&
		resp.StatusCode != StatusCodeRetryTemporaryRedirect {
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
func isThrottleResponse(resp *http.Response, errCode int) (bool, time.Duration) {
	if errCode == StatusCodeThrottle {
		return true, 0
	}
	if resp == nil || resp.StatusCode != StatusCodeThrottle {
		return false, 0
	}
	retryAfterStr := resp.Header.Get("Retry-After")
	if retryAfterStr == "" {
		return true, 0
	}
	// Retry-After can be either a Date or a number of seconds; look for both.
	if retryAfterSec, err := strconv.Atoi(retryAfterStr); err == nil {
		return true, time.Duration(retryAfterSec) * time.Second
	}
	retryAfterDate, err := http.ParseTime(retryAfterStr)
	if err != nil {
		// Unable to parse non-zero header as something we recognize...
		return true, 0
	}
	return true, time.Until(retryAfterDate)
}

func isUnavailableResponse(resp *http.Response, errCode int) bool {
	if errCode == StatusCodeUnavailable {
		return true
	}
	if resp == nil || resp.StatusCode != StatusCodeUnavailable {
		return false
	}
	return true
}

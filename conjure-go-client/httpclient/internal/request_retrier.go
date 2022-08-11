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

package internal

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/palantir/pkg/retry"
)

// RequestRetrier manages the lifecylce of a single request. It will tracks the
// backoff timing between subsequent requests. The retrier should only suggest
// a retry if the previous request returned a redirect or is a mesh URI. In the
// case of a mesh URI being detected, the request retrier will only attempt the
// request once.
type RequestRetrier struct {
	retrier retry.Retrier

	maxAttempts  int
	attemptCount int
}

// NewRequestRetrier creates a new request retrier.
// Regardless of maxAttempts, mesh URIs will never be retried.
func NewRequestRetrier(
	retrier retry.Retrier,
	maxAttempts int,
) *RequestRetrier {
	return &RequestRetrier{
		retrier:      retrier,
		maxAttempts:  maxAttempts,
		attemptCount: 0,
	}
}

func (r *RequestRetrier) attemptsRemaining() bool {
	// maxAttempts of 0 indicates no limit
	if r.maxAttempts == 0 {
		return true
	}
	return r.attemptCount < r.maxAttempts
}

// Next returns true if a subsequent request attempt should be attempted. If uses the previous response/resp err (if
// provided) to determine if the request should be attempted. If the returned value is true, the retrier will have
// waited the desired backoff interval before returning when applicable. If the previous response was a redirect, the
// retrier will also return the URL that should be used for the new next request.
func (r *RequestRetrier) Next(resp *http.Response, err error) (bool, *url.URL) {
	defer func() { r.attemptCount++ }()
	if r.isSuccess(resp) {
		return false, nil
	}

	if r.isNonRetryableClientError(resp, err) {
		return false, nil
	}

	// handle redirects
	if tryOther, otherURI := isRetryOtherResponse(resp, err); tryOther && otherURI != nil {
		return true, otherURI
	}

	// don't retry mesh uris
	if r.isMeshURI(resp) {
		return false, nil
	}

	if !r.attemptsRemaining() {
		// Retries exhausted
		return false, nil
	}
	return r.retrier.Next(), nil
}

func (*RequestRetrier) isSuccess(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	// Check for a 2XX status
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func (*RequestRetrier) isNonRetryableClientError(resp *http.Response, err error) bool {
	errCode, _ := StatusCodeFromError(err)
	// Check for a 4XX status parsed from the error or in the response
	if isClientError(errCode) && errCode != StatusCodeThrottle {
		return false
	}
	if resp != nil && isClientError(resp.StatusCode) {
		// 429 is retryable
		if isThrottle, _ := isThrottleResponse(resp, errCode); !isThrottle {
			return false
		}
		return true
	}
	return false
}

func (*RequestRetrier) isMeshURI(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	return strings.HasPrefix(getBaseURI(resp.Request.URL), meshSchemePrefix)
}

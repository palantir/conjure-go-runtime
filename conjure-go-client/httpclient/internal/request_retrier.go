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
	"strings"

	"github.com/palantir/pkg/retry"
)

// RequestRetrier manages the lifecylce of a single request. It will tracks the
// backoff timing between subsequent requests. The retrier should only suggest
// a URI if the previous request returned a redirect or is a mesh URI. In the
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

// Next returns true if a subsequent request attempt should be attempted. If
// uses the previous requestURI to determine if the request should be
// attempted. If the returned value is true, the retrier will have waited the
// desired backoff interval before returning.
func (r *RequestRetrier) Next(prevReq *http.Request, prevResp *http.Response) bool {
	defer func() { r.attemptCount++ }()
	// check for bad requests
	if prevResp != nil {
		prevCode := prevResp.StatusCode
		// succesfull response
		if prevCode == http.StatusOK {
			return false
		}
		if prevCode >= http.StatusBadRequest && prevCode < http.StatusInternalServerError {
			return false
		}
	}

	// don't retry mesh uris
	if prevReq != nil {
		prevURI := getBaseURI(prevReq.URL)
		if r.isMeshURI(prevURI) {
			return false
		}
	}

	if !r.attemptsRemaining() {
		// Retries exhausted
		return false
	}
	return r.retrier.Next()
}

func (*RequestRetrier) isMeshURI(uri string) bool {
	return strings.HasPrefix(uri, meshSchemePrefix)
}

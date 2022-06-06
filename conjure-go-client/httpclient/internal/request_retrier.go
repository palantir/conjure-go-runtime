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
	uris          []string
	offset        int
	relocatedURIs map[string]struct{}
	maxAttempts   int
	attemptCount  int
}

// NewRequestRetrier creates a new request retrier.
// Regardless of maxAttempts, mesh URIs will never be retried.
func NewRequestRetrier(uris []string, maxAttempts int) *RequestRetrier {
	offset := 0
	return &RequestRetrier{
		currentURI:    uris[offset],
		uris:          uris,
		offset:        offset,
		relocatedURIs: map[string]struct{}{},
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
		// First attempt is always successful
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
	nextURI := r.getNextURI(resp, respErr)
	if nextURI == "" {
		// The previous response was not retryable
		return "", false
	}
	return nextURI, r.isRelocatedURI(nextURI)
}

func (r *RequestRetrier) getNextURI(resp *http.Response, respErr error) string {
	errCode, _ := StatusCodeFromError(respErr)
	// 2XX and 4XX responses (except 429) are not retryable
	if isSuccess(resp) || isNonRetryableClientError(resp, errCode) {
		return ""
	}
	// 307 or 308: go to particular node if provided
	if shouldTryOther, otherURI := isRetryOtherResponse(resp, respErr, errCode); shouldTryOther && otherURI != nil {
		r.setRelocatedURI(otherURI)
	} else {
		r.nextURI()
	}
	return r.currentURI
}

func isSuccess(resp *http.Response) bool {
	// Check for a 2XX status
	return resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300
}

func isNonRetryableClientError(resp *http.Response, errCode int) bool {
	// Check for a 4XX status parsed from the error or in the response
	if isClientError(errCode) || (resp != nil && isClientError(resp.StatusCode)) {
		// 429 is retryable
		if isThrottle, _ := isThrottleResponse(resp, errCode); !isThrottle {
			return true
		}
	}
	return false
}

func (r *RequestRetrier) setRelocatedURI(uri *url.URL) {
	// If the URI returned by relocation header is a relative path we will resolve it with the current URI
	if !uri.IsAbs() {
		if currentURI := parseLocationURL(r.currentURI); currentURI != nil {
			uri = currentURI.ResolveReference(uri)
		}
	}
	nextURI := uri.String()
	r.relocatedURIs[uri.String()] = struct{}{}
	r.currentURI = nextURI
}

func (r *RequestRetrier) nextURI() {
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

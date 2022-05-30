// Copyright (c) 2021 Palantir Technologies. All rights reserved.
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
	"github.com/palantir/conjure-go-runtime/v2/conjure-go-client/httpclient"
	"net/http"
)

type backoffMiddleware struct {
	backoff  func()
	seenUris map[string]interface{}
}

// NewBackoffMiddleware returns a Middleware that implements backoff for URIs that have already been seen.
// The backoff function is expected to block for the desired backoff duration.
func NewBackoffMiddleware(backoff func()) httpclient.Middleware {
	return &backoffMiddleware{
		backoff:  backoff,
		seenUris: make(map[string]interface{}),
	}
}

func (b *backoffMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	baseURI := getBaseURI(req.URL)
	_, seen := b.seenUris[baseURI]
	if seen {
		b.backoff()
	}
	return next.RoundTrip(req)
}
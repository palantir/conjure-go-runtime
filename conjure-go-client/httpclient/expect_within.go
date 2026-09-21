// Copyright (c) 2026 Palantir Technologies. All rights reserved.
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

package httpclient

import (
	"net/http"
	"time"

	"github.com/palantir/conjure-go-runtime/v3/conjure-go-client/httpclient/deadlines"
	"github.com/palantir/pkg/refreshable/v2"
)

// expectWithinMiddleware is a middleware that:
// 1. Checks if the deadline has expired before making a request
// 2. Propagates the remaining deadline as a header on outbound requests
type expectWithinMiddleware struct {
	enforcement refreshable.Refreshable[deadlines.Enforcement]
	timeout     refreshable.Refreshable[time.Duration]
}

func newExpectWithinMiddleware(enforcement refreshable.Refreshable[deadlines.Enforcement], timeout refreshable.Refreshable[time.Duration]) Middleware {
	return &expectWithinMiddleware{
		enforcement: enforcement,
		timeout:     timeout,
	}
}

func (m *expectWithinMiddleware) RoundTrip(req *http.Request, next http.RoundTripper) (*http.Response, error) {
	// encode the deadline in the request
	if err := deadlines.EncodeToRequest(req.Context(), m.getProposedDeadline(), req, m.enforcement.Current()); err != nil {
		return nil, err
	}

	return next.RoundTrip(req)
}

// getProposedDeadline returns the proposed deadline for a call. Returns the client's connection timeout value or, if
// that value is <=0, returns a large value (1 day).
func (m *expectWithinMiddleware) getProposedDeadline() time.Duration {
	// start proposed deadline as connection timeout value
	proposedDeadline := m.timeout.Current()

	// Match Java behavior: if timeout is <= 0, use a large value (1 day) rather than putting 0 on the wire.
	// https://github.com/palantir/dialogue/blob/c4856aeea7600a472dbb881d9544652a9f184dbf/dialogue-core/src/main/java/com/palantir/dialogue/core/DeadlineAdvertisementChannel.java#L59
	if proposedDeadline <= 0 {
		proposedDeadline = 24 * time.Hour
	}
	return proposedDeadline
}

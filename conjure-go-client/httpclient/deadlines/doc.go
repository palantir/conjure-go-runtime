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

// Package deadlines provides utilities for working with request deadlines using the
// Expect-Within header mechanism.
//
// This package is a translation from Java to Go of https://github.com/palantir/deadlines-java/tree/32782ae7ddf47757e0840ce843720329abf32b3b/deadlines/src/main/java/com/palantir/deadlines
// performed by Claude at the 0.22.0 release.
//
// The deadlines package implements deadline propagation across service boundaries using
// HTTP headers. It supports:
//   - Parsing and setting Expect-Within headers
//   - Deadline enforcement strategies (enforce, disable, defer)
//   - Deadline expiration error handling
//   - Integration with context.Context
//
// Example usage:
//
//	// Create a context with a deadline
//	ctx := deadlines.ContextWithDeadline(context.Background(), 5*time.Second, httpclient.EnforcementEnforce)
//
//	// Parse deadline from incoming request
//	if ewc := deadlines.ParseExpectWithinFromHeaders(req); ewc != nil {
//	    ctx = deadlines.ContextWithExpectWithin(ctx, *ewc)
//	}
//
//	// Check if deadline has expired
//	if deadlines.IsDeadlineExpired(ctx) {
//	    return deadlines.ErrDeadlineExpiredExternal
//	}
//
//	// Set deadline headers on outbound request
//	if ewc, ok := deadlines.GetExpectWithinFromContext(ctx); ok {
//	    deadlines.SetExpectWithinHeaders(outboundReq, ewc)
//	}
//
//	// Handle deadline expiration in response
//	if err := doWork(); err != nil {
//	    if deadlines.IsDeadlineExpiredError(err) {
//	        deadlines.EncodeDeadlineExpiredToResponse(err, w)
//	        return
//	    }
//	}
package deadlines

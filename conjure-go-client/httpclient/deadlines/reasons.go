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

package deadlines

import (
	"net/http"
	"strings"
)

// EncodeDeadlineExpiredToResponse encodes a deadline expiration error to an HTTP response.
// This sets the Deadline-Expired-Reason header and appropriate HTTP status code.
func EncodeDeadlineExpiredToResponse(err error, w http.ResponseWriter) bool {
	reason := GetDeadlineExpiredReason(err)
	if reason == nil {
		return false
	}

	w.Header().Set(HeaderDeadlineExpiredReason, reason.String())
	w.WriteHeader(reason.StatusCode())
	return true
}

// ParseDeadlineExpiredFromResponse parses a deadline expiration error from an HTTP response.
// Returns the appropriate error if the response indicates a deadline expiration, nil otherwise.
func ParseDeadlineExpiredFromResponse(resp *http.Response) error {
	reasonHeader := resp.Header.Get(HeaderDeadlineExpiredReason)
	if reasonHeader == "" {
		return nil
	}

	statusCode := resp.StatusCode

	switch strings.ToLower(reasonHeader) {
	case "external":
		if statusCode == 400 {
			return ErrDeadlineExpiredExternal
		}
	case "internal":
		if statusCode == 500 {
			return ErrDeadlineExpiredInternal
		}
	}

	return nil
}

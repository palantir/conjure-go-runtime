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
	"errors"
)

// DeadlineExpiredReason indicates the reason for a deadline expiration.
type DeadlineExpiredReason int

const (
	// ReasonExternal indicates an externally provided deadline expired.
	// This is considered a client error (HTTP 400).
	ReasonExternal DeadlineExpiredReason = iota

	// ReasonInternal indicates an internally imposed deadline expired.
	// This is considered a server error (HTTP 500).
	ReasonInternal
)

// String returns the string representation of the reason for use in headers.
func (r DeadlineExpiredReason) String() string {
	switch r {
	case ReasonExternal:
		return "external"
	case ReasonInternal:
		return "internal"
	default:
		return ""
	}
}

// StatusCode returns the HTTP status code associated with this reason.
func (r DeadlineExpiredReason) StatusCode() int {
	switch r {
	case ReasonExternal:
		return 400
	case ReasonInternal:
		return 500
	default:
		return 0
	}
}

// deadlineExpiredError is a common base for deadline expiration errors.
type deadlineExpiredError struct {
	reason  DeadlineExpiredReason
	message string
}

func (e *deadlineExpiredError) Error() string {
	return e.message
}

func (e *deadlineExpiredError) Reason() DeadlineExpiredReason {
	return e.reason
}

var (
	// ErrDeadlineExpiredExternal indicates an externally provided deadline expired.
	ErrDeadlineExpiredExternal = &deadlineExpiredError{
		reason:  ReasonExternal,
		message: "An externally provided deadline for completing work has expired.",
	}

	// ErrDeadlineExpiredInternal indicates an internally imposed deadline expired.
	ErrDeadlineExpiredInternal = &deadlineExpiredError{
		reason:  ReasonInternal,
		message: "An internal deadline for completing work has expired.",
	}
)

// IsDeadlineExpiredExternal returns true if the error is an external deadline expiration.
func IsDeadlineExpiredExternal(err error) bool {
	return errors.Is(err, ErrDeadlineExpiredExternal)
}

// IsDeadlineExpiredInternal returns true if the error is an internal deadline expiration.
func IsDeadlineExpiredInternal(err error) bool {
	return errors.Is(err, ErrDeadlineExpiredInternal)
}

// IsDeadlineExpiredError returns true if the error is any kind of deadline expiration.
func IsDeadlineExpiredError(err error) bool {
	return IsDeadlineExpiredExternal(err) || IsDeadlineExpiredInternal(err)
}

// GetDeadlineExpiredReason returns the reason for the deadline expiration, if applicable.
// Returns nil if the error is not a deadline expiration error.
func GetDeadlineExpiredReason(err error) *DeadlineExpiredReason {
	if dErr, ok := errors.AsType[*deadlineExpiredError](err); ok {
		return &dErr.reason
	}
	return nil
}

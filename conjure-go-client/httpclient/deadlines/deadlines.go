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
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	// HeaderExpectWithin is the HTTP header name for the expect-within deadline.
	HeaderExpectWithin = "Expect-Within"

	// HeaderExpectWithinEnforced indicates whether deadline enforcement is requested.
	HeaderExpectWithinEnforced = "Expect-Within-Enforced"

	// HeaderDeadlineExpiredReason indicates the reason for a deadline expiration.
	HeaderDeadlineExpiredReason = "Deadline-Expired-Reason"
)

// Enforcement controls whether deadline expirations should be enforced.
type Enforcement int

const (
	// EnforcementDefer means defer enforcement decision to the inbound request or next hop.
	// No Expect-Within-Enforced header is sent.
	EnforcementDefer Enforcement = iota

	// EnforcementEnforce means enforce deadline expiration by throwing an error when expired.
	// Sets Expect-Within-Enforced header to "true".
	EnforcementEnforce

	// EnforcementDisable means disable deadline enforcement at this node and all downstream.
	// Sets Expect-Within-Enforced header to "false".
	EnforcementDisable
)

// ResolveWith resolves two enforcement strategies to determine the effective enforcement.
func (e Enforcement) ResolveWith(other Enforcement) Enforcement {
	switch e {
	case EnforcementDisable:
		return e
	case EnforcementEnforce:
		if other == EnforcementDisable {
			return other
		}
		return e
	case EnforcementDefer:
		return other
	default:
		return EnforcementDefer
	}
}

// ExpectWithinContext holds the expect-within deadline information.
type ExpectWithinContext struct {
	// Remaining is the deadline duration
	Remaining time.Duration
	// StartTime is the Unix timestamp in milliseconds when the header was received
	StartTime int64
	// Enforcement is the enforcement strategy for this deadline
	Enforcement Enforcement
	// Internal indicates whether this deadline originated internally (true) or externally from a request header (false)
	Internal bool
	// DisablePropagation prevents further propagation of this deadline when true
	DisablePropagation bool
}

// ParseExpectWithinFromHeaders parses the Expect-Within and Expect-Within-Enforced headers
// from an HTTP request and returns an ExpectWithinContext.
// Returns nil if no deadline header is present.
func ParseExpectWithinFromHeaders(r *http.Request) *ExpectWithinContext {
	deadlineHeader := r.Header.Get(HeaderExpectWithin)
	if deadlineHeader == "" {
		return nil
	}

	millis, err := parseExpectWithinHeader(deadlineHeader)
	if err != nil || millis < 0 {
		return nil
	}

	enforcement := parseEnforcementFromHeaders(
		r.Header.Get(HeaderExpectWithinEnforced),
		true,
	)

	return &ExpectWithinContext{
		Remaining:   millis,
		StartTime:   time.Now().UnixMilli(),
		Enforcement: enforcement,
	}
}

// SetExpectWithinHeaders sets the Expect-Within and Expect-Within-Enforced headers
// on an HTTP request based on the ExpectWithinContext.
func SetExpectWithinHeaders(r *http.Request, ewc ExpectWithinContext) {
	// Calculate remaining time
	now := time.Now().UnixMilli()
	elapsedMillis := now - ewc.StartTime
	elapsed := time.Duration(elapsedMillis) * time.Millisecond
	remaining := ewc.Remaining - elapsed

	if remaining <= 0 {
		// Deadline already expired, don't set headers
		return
	}

	// Set Expect-Within header
	r.Header.Set(HeaderExpectWithin, durationToHeaderValue(remaining))

	// Set Expect-Within-Enforced header if needed
	enforcementHeader := formatEnforcementHeader(ewc.Enforcement)
	if enforcementHeader != "" {
		r.Header.Set(HeaderExpectWithinEnforced, enforcementHeader)
	}
}

// checkExpiration checks if a deadline has expired and returns an error if enforcement is enabled.
// This function matches the behavior of the Java implementation.
//
// The disablePropagation and alreadyExpired parameters are used in the Java implementation for
// metrics recording with different intent values (IGNORE, PROPAGATE, PROPAGATE_ALREADY_EXPIRED).
// In this Go implementation, we don't yet have metrics but keep the parameters for future use.
func checkExpiration(deadline time.Duration, internal, disablePropagation, alreadyExpired, enforced bool) error {
	if deadline > 0 {
		return nil
	}

	// Deadline has expired

	// When not enforced, we don't throw but could record metrics here.
	// The intent would be:
	//   - IGNORE if disablePropagation is true
	//   - PROPAGATE_ALREADY_EXPIRED if alreadyExpired is true
	//   - PROPAGATE otherwise

	if !enforced && !disablePropagation {
		// Would record metrics here with appropriate intent
	}

	if enforced {
		// Return an error when enforcement is enabled
		if internal {
			return ErrDeadlineExpiredInternal
		}
		return ErrDeadlineExpiredExternal
	}

	return nil
}

// EncodeToRequest encodes a deadline into request headers.
//
// The actual deadline value encoded will be the minimum of:
//   - the proposedDeadline parameter
//   - the remaining deadline from the context (if it exists via GetExpectWithinFromContext)
//
// This ensures that the deadline set for the request will be based on the remaining deadline from
// already-set internal state, or a smaller one if the caller chooses that.
//
// The client requested enforcement strategy will be resolved against the internal state in the following manner:
//   - If either client or internal state requests Enforcement.DISABLE, then the deadline is not enforced.
//   - Then, if either client or internal state requests Enforcement.ENFORCE, then the deadline is enforced.
//   - Finally, if both the client and internal state requests Enforcement.DEFER, the deadline is not
//     enforced and an Expect-Within-Enforced header is not encoded on the request.
//
// If the deadline has expired and enforcement is enabled, returns ErrDeadlineExpiredExternal or
// ErrDeadlineExpiredInternal depending on whether the deadline came from context or was proposed.
func EncodeToRequest(ctx context.Context, proposedDeadline time.Duration, r *http.Request, clientEnforcement Enforcement) error {
	stateDeadline, hasState := GetExpectWithinFromContext(ctx)

	if !hasState {
		// No state deadline, use proposedDeadline
		if err := checkExpiration(proposedDeadline, false, false, false, clientEnforcement == EnforcementEnforce); err != nil {
			return err
		}
		r.Header.Set(HeaderExpectWithin, durationToHeaderValue(proposedDeadline))
		if enforcementHeader := formatEnforcementHeader(clientEnforcement); enforcementHeader != "" {
			r.Header.Set(HeaderExpectWithinEnforced, enforcementHeader)
		}
		return nil
	}

	// State deadline exists, use the minimum of proposedDeadline and the one from state
	elapsed := time.Duration(time.Now().UnixMilli()-stateDeadline.StartTime) * time.Millisecond
	remainingState := stateDeadline.Remaining - elapsed

	resolvedEnforcement := stateDeadline.Enforcement.ResolveWith(clientEnforcement)
	enforced := resolvedEnforcement == EnforcementEnforce

	if proposedDeadline <= remainingState {
		// Use proposed deadline
		proposedDeadlineAlreadyExpired := proposedDeadline <= 0
		if err := checkExpiration(
			proposedDeadline,
			false, // proposed deadlines are external
			stateDeadline.DisablePropagation,
			proposedDeadlineAlreadyExpired,
			enforced,
		); err != nil {
			return err
		}
		if !stateDeadline.DisablePropagation {
			r.Header.Set(HeaderExpectWithin, durationToHeaderValue(proposedDeadline))
			if enforcementHeader := formatEnforcementHeader(resolvedEnforcement); enforcementHeader != "" {
				r.Header.Set(HeaderExpectWithinEnforced, enforcementHeader)
			}
		}
	} else {
		// Use state deadline
		stateDeadlineAlreadyExpired := stateDeadline.Remaining <= 0
		if err := checkExpiration(
			remainingState,
			stateDeadline.Internal,
			stateDeadline.DisablePropagation,
			stateDeadlineAlreadyExpired,
			enforced,
		); err != nil {
			return err
		}
		if !stateDeadline.DisablePropagation {
			r.Header.Set(HeaderExpectWithin, durationToHeaderValue(remainingState))
			enforcementHeader := formatEnforcementHeader(resolvedEnforcement)
			if enforcementHeader != "" {
				r.Header.Set(HeaderExpectWithinEnforced, enforcementHeader)
			}
		}
	}

	return nil
}

type ctxKey string

const expectWithinContextKey ctxKey = "expectWithin"

// GetExpectWithinFromContext retrieves the ExpectWithinContext from the context.
func GetExpectWithinFromContext(ctx context.Context) (ExpectWithinContext, bool) {
	val := ctx.Value(expectWithinContextKey)
	if val == nil {
		return ExpectWithinContext{}, false
	}
	ewc, ok := val.(ExpectWithinContext)
	return ewc, ok
}

// ContextWithExpectWithin returns a copy of the context with the ExpectWithinContext set.
func ContextWithExpectWithin(ctx context.Context, ewc ExpectWithinContext) context.Context {
	return context.WithValue(ctx, expectWithinContextKey, ewc)
}

// ContextWithDeadline creates a new context with an expect-within deadline.
// The deadline is the duration from now.
func ContextWithDeadline(ctx context.Context, deadline time.Duration, enforcement Enforcement) context.Context {
	ewc := ExpectWithinContext{
		Remaining:   deadline,
		StartTime:   time.Now().UnixMilli(),
		Enforcement: enforcement,
	}
	return ContextWithExpectWithin(ctx, ewc)
}

// GetRemainingDeadline returns the remaining time until the deadline expires.
// Returns 0 if the deadline has already expired or if no deadline is set.
func GetRemainingDeadline(expectWithin ExpectWithinContext) time.Duration {
	now := time.Now().UnixMilli()
	elapsedMillis := now - expectWithin.StartTime
	elapsed := time.Duration(elapsedMillis) * time.Millisecond
	remaining := expectWithin.Remaining - elapsed

	if remaining <= 0 {
		return 0
	}

	return remaining
}

// IsDeadlineExpired returns true if the deadline in the context has expired.
// Returns false if there is no deadline set.
func IsDeadlineExpired(ctx context.Context) bool {
	ewc, ok := GetExpectWithinFromContext(ctx)
	if !ok {
		return false
	}

	now := time.Now().UnixMilli()
	elapsedMillis := now - ewc.StartTime
	elapsed := time.Duration(elapsedMillis) * time.Millisecond
	remaining := ewc.Remaining - elapsed

	return remaining <= 0
}

// parseExpectWithinHeader parses the Expect-Within header value (in decimal seconds)
// and returns the duration.
func parseExpectWithinHeader(headerValue string) (time.Duration, error) {
	if headerValue == "" {
		return -1, nil
	}

	seconds, err := strconv.ParseFloat(headerValue, 64)
	if err != nil {
		return -1, fmt.Errorf("failed to parse expect-within header: %w", err)
	}

	if seconds < 0 {
		return -1, fmt.Errorf("expect-within header value cannot be negative")
	}

	millis := int64(math.Ceil(seconds * 1000))
	return time.Duration(millis) * time.Millisecond, nil
}

// durationToHeaderValue formats a duration in milliseconds to a decimal seconds string.
func durationToHeaderValue(duration time.Duration) string {
	if duration <= 0 {
		return "0"
	}

	seconds := float64(duration.Milliseconds()) / 1000.0
	return strconv.FormatFloat(seconds, 'f', 3, 64)
}

// parseEnforcementFromHeaders parses the Expect-Within-Enforced header.
func parseEnforcementFromHeaders(headerEnforced string, headerDeadlineSet bool) Enforcement {
	if !headerDeadlineSet || headerEnforced == "" {
		return EnforcementDefer
	}

	switch headerEnforced {
	case "true":
		return EnforcementEnforce
	case "false":
		return EnforcementDisable
	default:
		return EnforcementDefer
	}
}

// formatEnforcementHeader formats an Enforcement to a header value.
func formatEnforcementHeader(enforcement Enforcement) string {
	switch enforcement {
	case EnforcementDisable:
		return "false"
	case EnforcementEnforce:
		return "true"
	case EnforcementDefer:
		return ""
	default:
		return ""
	}
}

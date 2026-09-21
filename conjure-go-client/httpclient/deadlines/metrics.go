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
	"time"

	"github.com/palantir/pkg/metrics"
)

const (
	metricDeadlineExpired = "deadline.expired"
	metricTagCause        = "cause"
	metricTagIntent       = "intent"
	metricTagBudget       = "budget"
)

// ExpiredCause indicates whether a deadline expiration was caused internally or externally.
type ExpiredCause string

const (
	// ExpiredCauseInternal indicates a deadline expiration was caused by an internal process,
	// such as a server's inability to meet its own internal deadline even though a client
	// provided ample time.
	ExpiredCauseInternal ExpiredCause = "internal"

	// ExpiredCauseExternal indicates a deadline expiration was caused due to the inability
	// to meet an externally provided deadline, such as a server being unable to complete
	// required work before a client-provided deadline elapses.
	ExpiredCauseExternal ExpiredCause = "external"
)

// ExpiredIntent describes the intent (or not) to propagate an expired deadline on further RPC calls.
type ExpiredIntent string

const (
	// ExpiredIntentThrow indicates enforcement was enabled for this trace at the time the
	// deadline expiration was reached, and an error was returned.
	//
	// The "throw" nomenclature is used to match the Java metric: https://github.com/palantir/deadlines-java/blob/32782ae7ddf47757e0840ce843720329abf32b3b/deadlines/src/main/metrics/deadlines-metrics.yml#L25-L27
	ExpiredIntentThrow ExpiredIntent = "throw"

	// ExpiredIntentPropagate indicates deadline propagation was enabled for this trace at
	// the time the deadline expiration was reached. This means that further RPC calls will
	// still potentially propagate an expired deadline value.
	ExpiredIntentPropagate ExpiredIntent = "propagate"

	// ExpiredIntentPropagateAlreadyExpired indicates deadline propagation was enabled for
	// this trace, but the deadline had already expired by the time it was first received.
	ExpiredIntentPropagateAlreadyExpired ExpiredIntent = "propagate-already-expired"

	// ExpiredIntentIgnore indicates deadline propagation was disabled for this trace at the
	// time the deadline expiration was reached. This means that further RPC calls will not
	// propagate an expired deadline value any more.
	ExpiredIntentIgnore ExpiredIntent = "ignore"
)

// ExpiredBudget records the original deadline budget bucket at the time of a deadline expiration.
type ExpiredBudget string

const (
	// ExpiredBudgetSub100ms indicates the original deadline budget was less than 100 milliseconds.
	ExpiredBudgetSub100ms ExpiredBudget = "sub-100ms"

	// ExpiredBudgetSub1s indicates the original deadline budget was less than 1 second.
	ExpiredBudgetSub1s ExpiredBudget = "sub-1s"

	// ExpiredBudgetSub10s indicates the original deadline budget was less than 10 seconds.
	ExpiredBudgetSub10s ExpiredBudget = "sub-10s"

	// ExpiredBudgetSub100s indicates the original deadline budget was less than 100 seconds.
	ExpiredBudgetSub100s ExpiredBudget = "sub-100s"

	// ExpiredBudgetAbove100s indicates the original deadline budget was 100 seconds or more.
	ExpiredBudgetAbove100s ExpiredBudget = "above-100s"
)

// recordDeadlineExpired records a deadline expiration metric with the given tags.
func recordDeadlineExpired(ctx context.Context, cause ExpiredCause, intent ExpiredIntent, budget ExpiredBudget) {
	registry := metrics.FromContext(ctx)
	causeTag := metrics.MustNewTag(metricTagCause, string(cause))
	intentTag := metrics.MustNewTag(metricTagIntent, string(intent))
	budgetTag := metrics.MustNewTag(metricTagBudget, string(budget))
	registry.Meter(metricDeadlineExpired, causeTag, intentTag, budgetTag).Mark(1)
}

// budgetBucket returns the appropriate budget bucket for a given duration.
func budgetBucket(duration time.Duration) ExpiredBudget {
	if duration < 100*time.Millisecond {
		return ExpiredBudgetSub100ms
	} else if duration < time.Second {
		return ExpiredBudgetSub1s
	} else if duration < 10*time.Second {
		return ExpiredBudgetSub10s
	} else if duration < 100*time.Second {
		return ExpiredBudgetSub100s
	}
	return ExpiredBudgetAbove100s
}

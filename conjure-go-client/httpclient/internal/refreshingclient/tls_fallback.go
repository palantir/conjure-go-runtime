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

package refreshingclient

import (
	"context"
)

type tlsFallbackReporterKey struct{}

// WithTLSFallbackReporter registers a reporter function on the context. The provided function is retrieved and called
// using reportTLSFallback.
func WithTLSFallbackReporter(ctx context.Context, reporter func()) context.Context {
	return context.WithValue(ctx, tlsFallbackReporterKey{}, reporter)
}

// reportTLSFallback retrieves the reporter function registered on the provided context using WithTLSFallbackReporter
// and calls it if it is non-nil.
func reportTLSFallback(ctx context.Context) {
	if reporter, ok := ctx.Value(tlsFallbackReporterKey{}).(func()); ok {
		reporter()
	}
}

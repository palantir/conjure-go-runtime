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

package internal

import (
	"context"
	"time"
)

type requestTimeoutKey struct{}

// ContextWithRequestTimeout stores a per-attempt timeout used by the httpc
// client to override the client-level timeout for a single request. Shared
// between the httpc and httpclient packages; not part of either public API.
func ContextWithRequestTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, requestTimeoutKey{}, d)
}

// RequestTimeoutFromContext returns the timeout stored by
// [ContextWithRequestTimeout], if any.
func RequestTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	v, ok := ctx.Value(requestTimeoutKey{}).(time.Duration)
	return v, ok
}
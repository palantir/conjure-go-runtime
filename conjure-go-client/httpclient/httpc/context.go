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

package httpc

import (
	"context"
	"time"
)

// rpcMethodNameKey is the context key for the RPC method name set by Endpoint.Execute.
type rpcMethodNameKey struct{}

// forUserAgentKey is the context key for the For-User-Agent header value.
type forUserAgentKey struct{}

// requestTimeoutKey is the context key for per-request timeout overrides.
type requestTimeoutKey struct{}

// RPCMethodName extracts the RPC method name from the context, if set by Endpoint.Execute.
func RPCMethodName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(rpcMethodNameKey{}).(string)
	return v, ok
}

// ContextWithRPCMethodName returns a new context with the RPC method name set for use in logging and metrics.
func ContextWithRPCMethodName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, rpcMethodNameKey{}, name)
}

// ContextWithForUserAgent returns a new context with the For-User-Agent header value set.
func ContextWithForUserAgent(ctx context.Context, forUserAgent string) context.Context {
	if forUserAgent == "" {
		return ctx
	}
	return context.WithValue(ctx, forUserAgentKey{}, forUserAgent)
}

func forUserAgentFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(forUserAgentKey{}).(string)
	return v, ok
}

// ContextWithRequestTimeout returns a new context carrying a per-request timeout
// that overrides the client-level timeout for a single request attempt. This is
// a low-level escape hatch; most callers should prefer [Overrides.WithTimeout]
// or [Endpoint.WithTimeout] instead, which use this internally.
func ContextWithRequestTimeout(ctx context.Context, d time.Duration) context.Context {
	return context.WithValue(ctx, requestTimeoutKey{}, d)
}

// requestTimeoutFromContext extracts a per-request timeout from the context, if set.
func requestTimeoutFromContext(ctx context.Context) (time.Duration, bool) {
	v, ok := ctx.Value(requestTimeoutKey{}).(time.Duration)
	return v, ok
}

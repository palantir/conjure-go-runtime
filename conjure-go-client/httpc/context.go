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
)

type (
	rpcMethodNameKey      struct{}
	forUserAgentKey       struct{}
	requestMiddlewaresKey struct{}
)

// contextWithRequestMiddlewares carries per-request middlewares from
// Endpoint.Execute to the baked seam that applies them inside telemetry.
func contextWithRequestMiddlewares(ctx context.Context, middlewares []Middleware) context.Context {
	return context.WithValue(ctx, requestMiddlewaresKey{}, middlewares)
}

func requestMiddlewaresFromContext(ctx context.Context) []Middleware {
	middlewares, _ := ctx.Value(requestMiddlewaresKey{}).([]Middleware)
	return middlewares
}

// RPCMethodName returns the RPC name set on ctx by Endpoint.Execute, if any.
func RPCMethodName(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(rpcMethodNameKey{}).(string)
	return v, ok
}

// ContextWithRPCMethodName stores the RPC name on ctx for logging and metrics.
func ContextWithRPCMethodName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, rpcMethodNameKey{}, name)
}

// ContextWithForUserAgent stores a For-User-Agent header value on ctx; the
// tracing middleware sets the header on outgoing requests that don't already have it.
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
